package cli

import (
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Manual pages (cream(1), cream-extension.yaml(5)) are rendered from the same
// command table as --help: synopses come from Command.Usage, flag names,
// placeholders and defaults from the flag sets that the dispatcher parses.
// Only the English prose lives here (help is Russian, man pages are English);
// man_test.go fails when a command or flag has no prose or prose has no
// command or flag. tools/genman writes the gzipped pages for the .deb.

// manCommand is the English description of one command.
type manCommand struct {
	Summary string
	Text    string
}

// manCommands describes every command of App.commands(), by name.
var manCommands = map[string]manCommand{
	"version": {"print the cream version and the contract",
		"Prints the cream version and the manifest contract it implements (cream/v1). " +
			"\\fBcream \\-\\-version\\fR is the same."},
	"source": {"manage extension catalog sources",
		"Sources are catalogs of extensions: a tar.gz archive pinned by sha256 or a local directory. " +
			"\\fBadd\\fR records a source in sources.yaml, \\fBremove\\fR deletes it and its cache, " +
			"\\fBlist\\fR and \\fBshow\\fR print them, \\fBsync\\fR fetches and unpacks them into the cache. " +
			"Synchronisation is the only place where cream uses the network."},
	"validate": {"validate a catalog and extension directories",
		"Checks cream-catalog.yaml of the catalog in \\fB\\-\\-catalog\\fR and every extension directory given: " +
			"manifest schema, directory name, executable bits, links, hooks declared versus present. " +
			"Suspicious calls in scripts are reported as warnings."},
	"catalog": {"pack a catalog into a deterministic archive",
		"\\fBcatalog pack\\fR writes a reproducible tar.gz of a catalog directory: sorted entries, " +
			"owner 0/0, mtime from \\fBSOURCE_DATE_EPOCH\\fR (required)."},
	"resolve": {"resolve layers offline",
		"Resolves extension references with their parameters into an ordered list of layers " +
			"for the given phase and architectures, without touching the machine. " +
			"With \\fB\\-\\-json\\fR the result is the record.json consumed by \\fBinstall \\-\\-phase build\\fR."},
	"install": {"install extensions",
		"Installs extensions from synchronised sources: resolves dependencies, checks platform, targets and conflicts, " +
			"places the tree under /opt/cream/ext, writes settings from KEY=VALUE arguments or \\fB\\-\\-params\\-file\\fR " +
			"and runs the install hook. KEY=\\- prompts for the value on stdin. " +
			"An unchanged extension is not reinstalled. " +
			"\\fB\\-\\-phase build\\fR installs one layer inside an image build (virt\\-customize) from \\fB\\-\\-from\\-dir\\fR."},
	"upgrade": {"upgrade extensions to the version in the source cache",
		"Upgrades the named extensions (or all installed ones with \\fB\\-\\-all\\fR) to the version " +
			"found in the source cache of the source each was installed from. " +
			"cream first builds a plan, one line per extension: \\fBupgrade\\fR from \\(-> to, " +
			"\\fBup\\-to\\-date\\fR (nothing newer: no hook, no platform or risk check), \\fBheld\\fR " +
			"(see \\fBhold\\fR) or \\fBblocked\\fR with a reason: \\fBrisk\\fR (targets.live: confirm without " +
			"\\fB\\-\\-accept\\-risk\\fR), \\fBplatform\\fR, \\fBdependency\\fR, \\fBsource\\fR, \\fBversion\\fR. " +
			"Upgrades then run in dependency order (providers first) and run the upgrade hook. " +
			"A blocked or failed extension does not stop the others, except those that depend on it; " +
			"the summary counts upgraded, unchanged, skipped and failed extensions, and the exit status is the code " +
			"of the worst outcome (a failure before a blocked one). " +
			"On a terminal (stdin and stderr) without \\fB\\-y\\fR, \\fB\\-\\-dry\\-run\\fR or \\fB\\-\\-json\\fR " +
			"cream asks y/N about each targets.live: confirm extension and about the plan; a no to the plan exits 1 " +
			"(upgrade.declined) with nothing changed. With one extension name the result and the error are those of that extension."},
	"hold": {"hold extensions at their installed version",
		"Marks installed extensions as held in their registry entry: \\fBupgrade \\-\\-all\\fR skips them (\\fBheld\\fR), " +
			"\\fBupgrade\\fR <ext> and \\fBinstall\\fR of another version refuse with exit status 68 (hold.held). " +
			"\\fBlist\\fR shows held extensions."},
	"unhold": {"release held extensions", "Removes the hold set by \\fBhold\\fR; the next \\fBupgrade\\fR upgrades them again."},
	"remove": {"remove an extension",
		"Runs the remove hook, removes the extension tree, its files and its registry entry. " +
			"Settings and state are kept unless \\fB\\-\\-purge\\fR is given."},
	"list": {"list installed extensions", "Prints installed extensions with their versions and sources."},
	"show": {"show the settings of an extension", "Prints the settings of an installed extension; secrets are masked."},
	"get":  {"print the value of a setting", "Prints the value of one key. Secrets are printed only with \\fB\\-\\-secret\\fR and only to root."},
	"set": {"change settings of an extension",
		"Writes KEY=VALUE pairs (KEY=\\- reads the value from stdin) into /etc/cream/<ext>/config or secrets " +
			"and applies them by restarting the configure unit, unless \\fB\\-\\-no\\-apply\\fR is given."},
	"config": {"settings for hooks and scripts",
		"\\fBconfig get\\fR prints one value; \\fBconfig export \\-\\-shell\\fR prints KEY='value' lines for a POSIX shell."},
	"run": {"run an extension action",
		"Runs an action declared by the extension. cream flags are accepted only before <ext>: " +
			"everything after the action name is passed to the action verbatim. " +
			"The exit code is the code of the action (0\\(en63)."},
	"actions":   {"list the actions of an extension", "Prints the actions declared by an installed extension with their parameters."},
	"configure": {"run the configure hook", "Restarts the configure unit cream\\-configure@<ext>.service, which runs the configure hook. \\fB\\-\\-from\\-unit\\fR is used by the unit itself."},
	"system":    {"initialise the system layout", "\\fBsystem init\\fR creates cream directories, installs the configure unit template and the configure targets."},
}

// manGlobalFlags describes the global flags (Globals.register), by name.
var manGlobalFlags = map[string]string{
	"json":         "Print exactly one JSON object (the envelope) to stdout; human-readable output and hook output go to stderr.",
	"root":         "Relocate every path under the absolute directory \\fIDIR\\fR (environment \\fBCREAM_ROOT\\fR).",
	"sources":      "Use the sources file \\fIFILE\\fR instead of /etc/cream/sources.yaml (environment \\fBCREAM_SOURCES\\fR).",
	"cache":        "Use the source cache directory \\fIDIR\\fR (environment \\fBCREAM_CACHE\\fR).",
	"lock-timeout": "How long to wait for a busy lock; negative values are rejected.",
	"q":            "Fewer messages on stderr.",
	"v":            "More messages on stderr.",
}

// manFlags describes the own flags of each command, by command then flag name.
// Reserved flags (reserveFlags) are described automatically.
var manFlags = map[string]map[string]string{
	"source": {
		"archive": "add: catalog archive \\fIURL\\fR, https:// or file:///.",
		"sha256":  "add: sha256 of the archive; required with \\fB\\-\\-archive\\fR.",
		"path":    "add: catalog directory (absolute path); it is copied, not linked.",
		"replace": "add: replace an existing source with different fields.",
		"sync":    "add: synchronise at once; on failure the source is not saved.",
	},
	"validate": {"catalog": "Catalog directory containing cream-catalog.yaml."},
	"catalog":  {"out": "pack: output archive file."},
	"resolve": {
		"phase":       "Phase: \\fBbuild\\fR or \\fBlive\\fR.",
		"target-arch": "Target architecture (default: this machine).",
		"host-arch":   "Build host architecture (default: this machine).",
		"family":      "Package family of the target (deb); without it the family is not checked in the build phase.",
		"installed":   "build: JSON array of extensions already in the base image ([{name, version, provides?}]).",
	},
	"install": {
		"source":             "Look up references without an explicit source in source \\fIS\\fR.",
		"reinstall":          "Run the install hook even if nothing changed.",
		"accept-risk":        "Allow extensions with targets.live: confirm.",
		"allow-skip":         "Treat hook exit code 20 as status skipped instead of an error.",
		"params-file":        "Read KEY=VALUE parameters from file \\fIF\\fR (mode 0600, owned by root).",
		"phase":              "Phase: \\fBlive\\fR or \\fBbuild\\fR (build only with \\fB\\-\\-from\\-dir\\fR).",
		"from-dir":           "build: extension tree \\fID\\fR (incoming/<id>/<name>).",
		"record":             "build: record.json \\fIF\\fR, a layer of \\fBcream resolve \\-\\-json\\fR.",
		"remove-params-file": "build: delete \\fB\\-\\-params\\-file\\fR after reading it.",
	},
	"upgrade": {
		"all":         "Upgrade every installed extension that is not held.",
		"dry-run":     "Print the plan and change nothing; the exit status is that of the plan (blocked extensions).",
		"y":           "Do not ask for confirmation on a terminal; targets.live: confirm still needs \\fB\\-\\-accept\\-risk\\fR.",
		"yes":         "Same as \\fB\\-y\\fR.",
		"fail-fast":   "Stop at the first blocked extension of the plan (nothing is upgraded) or at the first failed upgrade (the rest are not run).",
		"accept-risk": "Upgrade the extension \\fIext\\fR although it has targets.live: confirm; repeat it or separate names with commas, \\fBall\\fR accepts every one.",
	},
	"remove": {
		"purge":  "Also delete /etc/cream/<ext> and /var/lib/cream/ext/<ext>.",
		"forget": "Skip the remove hook: drop only the registry entry, the tree and cream artifacts.",
		"force":  "Remove even if installed extensions depend on it.",
	},
	"get": {"secret": "Print the value of a secret (root only)."},
	"set": {
		"apply":          "Apply the changes (default).",
		"no-apply":       "Only write the settings, do not apply them.",
		"wait":           "Wait until the configure unit has been restarted.",
		"allow-readonly": "Allow writing readonly keys.",
		"params-file":    "Read KEY=VALUE pairs from file \\fIF\\fR (mode 0600, owned by root).",
	},
	"config": {
		"secret": "config get: print the value of a secret (root only).",
		"shell":  "config export: print KEY='value' lines for a POSIX shell.",
	},
	"configure": {
		"wait":      "Wait until the configure unit restart has finished.",
		"from-unit": "Called from the cream\\-configure@ unit (unit only).",
	},
}

// manExitCodes is EXIT STATUS of cream(1) (spec §11.3); man_test.go checks
// that every cerr.Exit* constant is listed.
var manExitCodes = []struct {
	Code string
	Text string
}{
	{"0", "Success."},
	{"1", "A check did not pass (check.failed); \\fBupgrade\\fR: the plan was declined on a terminal (upgrade.declined)."},
	{"0\\(en63", "\\fBcream run\\fR: the exit code of the action itself."},
	{"64", "Usage error: flags, arguments, a secret in argv, a reserved command or flag (usage.*)."},
	{"65", "Invalid data: manifest, catalog, setting value, readonly key, record.json (manifest.*, config.*)."},
	{"66", "Not found: extension, source, key, action, manifest; @version mismatch (not_found.*)."},
	{"67", "Dependency graph: missing dependency, version, capability, cycle, conflict, file ownership, dependents, a provider whose upgrade failed (deps.*, conflict.*)."},
	{"68", "Not applicable: architecture, family, OS, commands, targets, cream version, hook returned 20, a held extension (platform.*, target.*, hold.held)."},
	{"69", "Source error: not synchronised, sha256 mismatch, network, archive (source.*)."},
	{"70", "Internal error of cream."},
	{"74", "Input/output or registry error (io.*, state.*)."},
	{"75", "Lock busy, timed out or wait interrupted (lock.*)."},
	{"76", "An action violated the protocol: exit code above 63 or not one JSON object (action.protocol)."},
	{"77", "Root required or unsafe file permissions (perm.*)."},
	{"79", "A hook or an action failed, did not start or timed out (hook.failed, action.*)."},
}

// ManPages renders the manual pages for version (the .TH footer) and date
// (SOURCE_DATE_EPOCH of the build). Keys are file names: cream.1 and
// cream-extension.yaml.5.
func (a App) ManPages(version string, date time.Time) map[string]string {
	return map[string]string{
		"cream.1":                creamPage(a.commands(), version, date),
		"cream-extension.yaml.5": extensionPage(version, date),
	}
}

func th(name, section, version string, date time.Time, title string) string {
	return fmt.Sprintf(".TH %s %s %s \"cream %s\" \"%s\"\n", name, section, date.UTC().Format("2006-01-02"),
		roffEscape(version), title)
}

func creamPage(cmds []Command, version string, date time.Time) string {
	var b strings.Builder
	b.WriteString(th("CREAM", "1", version, date, "User Commands"))
	b.WriteString(".SH NAME\ncream \\- extension executor for Debian machines and image builds\n")
	b.WriteString(".SH SYNOPSIS\n.B cream\n.RI [ global\\-options ] \" command \" [ arguments ...]\n")
	b.WriteString(".br\n.B cream help\n.RI [ command ]\n")
	b.WriteString(".SH DESCRIPTION\n")
	b.WriteString("\\fBcream\\fR reads \\fBextension.yaml\\fR manifests (see \\fBcream\\-extension.yaml\\fR(5)) " +
		"and runs the extension lifecycle (install, upgrade, configure, remove) on a live machine " +
		"and the build phase inside \\fBvirt\\-customize\\fR(1). It keeps the registry of installed extensions, " +
		"manages their settings and synchronises extension catalogs.\n")
	b.WriteString(".PP\nFlags may follow positional arguments; \\fB\\-\\-\\fR ends flag parsing. " +
		"\\fBcream help\\fR \\fIcommand\\fR, \\fB\\-h\\fR and \\fB\\-\\-help\\fR print the help of a command.\n")
	b.WriteString(".SH COMMANDS\n")
	for _, c := range cmds {
		doc := manCommands[c.Name]
		b.WriteString(".SS " + c.Name + "\n")
		for i, s := range synopses(c) {
			if i > 0 {
				b.WriteString(".br\n")
			}
			b.WriteString(".B cream " + roffEscape(s) + "\n")
		}
		b.WriteString(".PP\n" + doc.Text + "\n")
		if c.Flags != nil {
			fs := newFlagSet(c.Name)
			c.Flags(fs)
			writeFlags(&b, fs, manFlags[c.Name])
		}
	}
	b.WriteString(".SS reserved\nThe commands " + roffEscape(strings.Join(reservedCommands, ", ")) +
		" and the flags marked reserved are recognised but not implemented in cream 1.x: they exit with code 64 (usage.reserved).\n")
	b.WriteString(".SH GLOBAL OPTIONS\nGlobal options are accepted before the command and anywhere among its arguments.\n")
	gfs := newFlagSet("cream")
	g := Globals{LockTimeout: DefaultLockTimeout}
	g.register(gfs)
	writeFlags(&b, gfs, manGlobalFlags)
	b.WriteString(".TP\n.B \\-\\-version\nSame as \\fBcream version\\fR.\n")
	b.WriteString(`.SH ENVIRONMENT
.TP
.B CREAM_ROOT
Root directory for every path, as \fB\-\-root\fR.
.TP
.B CREAM_SOURCES
Sources file, as \fB\-\-sources\fR.
.TP
.B CREAM_CACHE
Source cache directory, as \fB\-\-cache\fR.
.TP
.B SOURCE_DATE_EPOCH
Timestamp for \fBcream catalog pack\fR.
.TP
.BR XDG_CONFIG_HOME ", " XDG_CACHE_HOME ", " XDG_STATE_HOME
Without root and without \fB\-\-root\fR cream runs in user mode: sources in
$XDG_CONFIG_HOME/cream/sources.yaml, cache in $XDG_CACHE_HOME/cream/,
sources.lock in $XDG_STATE_HOME/cream/.
.SH FILES
.TP
.I /etc/cream/sources.yaml
Catalog sources.
.TP
.I /etc/cream/<ext>/config
Non-secret settings of an extension (systemd EnvironmentFile).
.TP
.I /etc/cream/<ext>/secrets
Secret settings, mode 0600.
.TP
.I /opt/cream/ext/<ext>/
Installed extension tree; working directory of hooks and units.
.TP
.I /var/lib/cream/installed/<ext>.json
Registry of installed extensions.
.TP
.I /var/lib/cream/history.jsonl
Operation log without secrets.
.TP
.I /var/lib/cream/lock/
Lock files (global and per extension).
.TP
.I /var/cache/cream/sources/
Source cache.
.TP
.I /usr/lib/systemd/system/cream\-configure@.service
Template unit that runs the configure hook.
.TP
.I /usr/lib/cream/guest/linux\-{amd64,arm64}/cream
Guest binaries for image builds.
.SH EXIT STATUS
`)
	for _, e := range manExitCodes {
		b.WriteString(".TP\n.B " + e.Code + "\n" + e.Text + "\n")
	}
	b.WriteString(`.SH EXAMPLES
Add a catalog archive, synchronise it and install an extension:
.PP
.nf
.RS
cream source add main \-\-archive https://example.org/catalog.tar.gz \-\-sha256 <hex> \-\-sync
cream install main/hello GREETING=hi
.RE
.fi
.PP
Change a setting and list installed extensions as JSON:
.PP
.nf
.RS
cream set hello GREETING=hello
cream list \-\-json
.RE
.fi
.PP
Review the upgrade plan, hold one extension and upgrade the rest, accepting the risk of one
targets.live: confirm extension:
.PP
.nf
.RS
cream upgrade \-\-all \-\-dry\-run
cream hold teleport
cream upgrade \-\-all \-y \-\-accept\-risk firewall
.RE
.fi
.SH SEE ALSO
\fBcream\-extension.yaml\fR(5), \fBsystemctl\fR(1), \fBvirt\-customize\fR(1)
.PP
https://github.com/iamletenkov/cream
`)
	return b.String()
}

// reservedCommands are the commands isReservedCommand answers 64 for, in
// man order; man_test.go checks both lists agree.
var reservedCommands = []string{"search", "info", "verify", "assume", "lineage", "check", "status", "path", "stage"}

// synopses returns the synopsis forms of c from its Usage: the part before
// "  — " of every line that starts with the command name.
func synopses(c Command) []string {
	var out []string
	for _, line := range strings.Split(c.Usage, "\n") {
		line = strings.TrimSpace(line)
		if line != c.Name && !strings.HasPrefix(line, c.Name+" ") {
			continue
		}
		s, _, _ := strings.Cut(line, "  —")
		out = append(out, strings.ReplaceAll(strings.TrimSpace(s), "…", "..."))
	}
	return out
}

// writeFlags renders the flags of fs as a .TP list, sorted by name.
func writeFlags(b *strings.Builder, fs *flag.FlagSet, docs map[string]string) {
	var all []*flag.Flag
	fs.VisitAll(func(f *flag.Flag) { all = append(all, f) })
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	for _, f := range all {
		dash := "\\-\\-"
		if len(f.Name) == 1 {
			dash = "\\-"
		}
		b.WriteString(".TP\n\\fB" + dash + roffEscape(f.Name) + "\\fR")
		name, _ := flag.UnquoteUsage(f)
		if !isBoolFlag(f) {
			switch name {
			case "string", "value":
				name = "VALUE"
			case "duration":
				name = "DURATION"
			}
			b.WriteString(" \\fI" + roffEscape(name) + "\\fR")
		}
		b.WriteString("\n")
		if _, ok := f.Value.(reservedFlag); ok {
			b.WriteString("Reserved in cream/v1, not implemented: exits with code 64.\n")
			continue
		}
		b.WriteString(docs[f.Name])
		if d := f.DefValue; d != "" && d != "false" && d != "0" && d != "0s" {
			b.WriteString(" Default: " + roffEscape(d) + ".")
		}
		b.WriteString("\n")
	}
}

// roffEscape makes s safe as roff text: backslashes and hyphens escaped.
func roffEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\e")
	return strings.ReplaceAll(s, "-", "\\-")
}

// extensionPage is cream-extension.yaml(5). The format has no machine-readable
// schema (the parser in internal/manifest and spec §4 are the source of
// truth), so this is a short reference that points to the documentation.
func extensionPage(version string, date time.Time) string {
	return th("CREAM-EXTENSION.YAML", "5", version, date, "File Formats") + `.SH NAME
cream\-extension.yaml, extension.yaml \- manifest of a cream extension (schema cream/v1)
.SH SYNOPSIS
.I <catalog>/<root>/<name>/extension.yaml
.SH DESCRIPTION
Every extension is a directory named after the extension that contains
\fBextension.yaml\fR, executable hooks in \fBhooks/\fR, actions in
\fBactions/\fR and any other files. The catalog root contains
\fBcream\-catalog.yaml\fR (fields \fBschema\fR, \fBname\fR, \fBversion\fR,
\fBroots\fR).
.PP
Parsing is strict: unknown fields are errors, except keys prefixed with
\fBx\-\fR at any level, which are ignored. A scalar is not accepted in place of
a list. \fBcream validate\fR checks a manifest and its directory.
.SH FIELDS
.TP
.B schema
Required, exactly \fBcream/v1\fR.
.TP
.B name
Required, [a\-z0\-9][a\-z0\-9._\-]{0,63}; equal to the directory name.
.TP
.B version
Required, semantic version X.Y.Z[\-pre].
.TP
.B summary
Required, one line of at most 120 characters.
.TP
.B cream
Required, range of cream versions, for example ">=1.0.0".
.TP
.B kind
\fBextension\fR (default) or \fBlibrary\fR (files only, no hooks).
.TP
.B platform
\fBfamily\fR (deb), \fBarch\fR (amd64, arm64), \fBos\fR (list of {id, version}
from /etc/os\-release), \fBcommands\fR (required in PATH before install).
.TP
.B targets
\fBbuild\fR: exec (default) or none; \fBlive\fR: yes (default), confirm or no;
\fBlive_note\fR is required with confirm and no.
.TP
.BR provides ", " requires
Lists of capabilities.
.TP
.B depends
List of {name, version?} dependencies by name with a version range.
.TP
.B conflicts
\fBextensions\fR (names) and \fBcapabilities\fR.
.TP
.B hooks
install, upgrade, configure, apply, remove: each {timeout?};
\fBconfigure\fR also has \fBstage\fR (early, system, cloud\-user, graphical)
and \fBafter\fR, \fBbefore\fR, \fBwants\fR.
.TP
.B config.keys
Settings of the extension: KEY \-> description (type, default, secret,
readonly, ...).
.TP
.B actions
Actions run by \fBcream run\fR.
.TP
.B files
List of {src, dst, mode?} placed and owned by cream.
.PP
Reserved in cream/v1 and rejected (manifest.reserved): the field \fBbin\fR,
the hooks \fBsecret\fR and \fBcheck\fR, \fBsecret: hook\fR of a key,
\fBtargets.build: files\fR.
.SH EXAMPLE
.nf
.RS
schema: cream/v1
name: hello
version: 1.0.0
summary: Example extension
cream: ">=1.0.0"
platform:
  family: deb
  arch: [amd64, arm64]
hooks:
  install: {}
.RE
.fi
.SH SEE ALSO
\fBcream\fR(1)
.PP
The full format, including key and action descriptions, is
docs/reference/manifest.md in https://github.com/iamletenkov/cream.
`
}
