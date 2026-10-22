package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/iamletenkov/cream/internal/engine"
)

// namesFlag is a repeatable flag (--accept-risk a --accept-risk b); commas
// separate names too.
type namesFlag []string

func (f *namesFlag) String() string { return strings.Join(*f, ",") }

func (f *namesFlag) Set(v string) error {
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			*f = append(*f, s)
		}
	}
	return nil
}

// upgradeFlags are the flags of cream upgrade.
type upgradeFlags struct {
	all, dryRun, yes, failFast bool
	acceptRisk                 namesFlag
	phase                      string
}

// upgradeCommand is `cream upgrade` (§6.5): the plan first, then the
// upgrades. One named extension keeps the 1.x form (its record, or the error
// of its blocked or failed upgrade); --all, several names and --dry-run give
// the plan form (golden upgrade-*.json) with the exit code of the worst outcome.
func upgradeCommand() Command {
	var f upgradeFlags
	return Command{Name: "upgrade",
		Usage: "upgrade [--phase build|live] [--dry-run] [-y] [--accept-risk <ext>|all]… [--fail-fast] <ext>… | --all" +
			"  — обновление до версии в кеше источника: сначала план, потом действия",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&f.all, "all", false, "обновить все установленные (закреплённые cream hold пропускаются)")
			fs.BoolVar(&f.dryRun, "dry-run", false, "только показать план, ничего не менять")
			fs.BoolVar(&f.yes, "y", false, "не спрашивать подтверждение в терминале (то же, что --yes)")
			fs.BoolVar(&f.yes, "yes", false, "не спрашивать подтверждение в терминале")
			fs.BoolVar(&f.failFast, "fail-fast", false, "остановиться на первом пропуске плана или первой ошибке")
			fs.StringVar(&f.phase, "phase", "", "фаза `P`: live или build; без флага — CREAM_PHASE родителя, иначе по PID 1")
			fs.Var(&f.acceptRisk, "accept-risk", "обновлять расширение `ext` с targets.live: confirm (all — все); можно повторять")
		},
		Run: func(ctx *Context, args []string) (any, error) { return runUpgrade(ctx, args, f) }}
}

func runUpgrade(ctx *Context, args []string, f upgradeFlags) (any, error) {
	if err := requireRoot(ctx, "upgrade"); err != nil {
		return nil, err
	}
	e, err := phaseEngine(ctx, f.phase)
	if err != nil {
		return nil, err
	}
	r := engine.UpgradeRequest{Names: args, All: f.all, AcceptRisk: f.acceptRisk, DryRun: f.dryRun,
		FailFast: f.failFast}
	if !f.yes && !f.dryRun && !ctx.Globals.JSON && ctx.App.Interactive != nil && ctx.App.Interactive() {
		p := &prompter{in: bufio.NewReader(ctx.App.Stdin), out: ctx.Stderr()}
		r.ConfirmRisk, r.Confirm = p.risk, p.plan
	}
	rep, err := e.Upgrade(ctx.Ctx, r)
	if err != nil {
		return nil, err
	}
	if !f.all && len(args) == 1 && !f.dryRun {
		if err := rep.Err(); err != nil {
			return nil, err
		}
		return installOutput(rep.Extensions, true), nil
	}
	return upgradeResult{rep}, nil
}

// prompter asks y/N on a terminal: the question to stderr, the answer from stdin.
type prompter struct {
	in  *bufio.Reader
	out io.Writer
}

func (p *prompter) ask(question string) (bool, error) {
	if _, err := fmt.Fprintf(p.out, "%s [y/N] ", question); err != nil {
		return false, err
	}
	line, err := p.in.ReadString('\n')
	if err != nil && line == "" {
		_, _ = fmt.Fprintln(p.out)
		return false, nil // EOF — no
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "д", "да":
		return true, nil
	}
	return false, nil
}

func (p *prompter) risk(name, note string) (bool, error) {
	return p.ask(fmt.Sprintf("%s: targets.live: confirm — %s\nОбновить на живой машине?", name, note))
}

func (p *prompter) plan(plan []engine.UpgradeItem) (bool, error) {
	if err := writeUpgradePlan(p.out, plan, true); err != nil {
		return false, err
	}
	n := 0
	for _, it := range plan {
		if it.Status == engine.StatusPlanned {
			n++
		}
	}
	return p.ask(fmt.Sprintf("Обновить расширений: %d. Продолжить?", n))
}

// upgradeResult is the plan form of upgrade (golden upgrade-*.json): the
// exit code is that of the worst outcome, ok = code == 0.
type upgradeResult struct {
	*engine.UpgradeReport
}

func (r upgradeResult) ExitCode() int { return r.Summary.ExitCode }

func (r upgradeResult) WriteText(w io.Writer) error {
	if err := writeUpgradePlan(w, r.Plan, r.DryRun); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, upgradeSummaryLine(r.Plan, r.Summary, r.DryRun))
	return err
}

// writeUpgradePlan prints one line per item: name, versions, what happens.
func writeUpgradePlan(w io.Writer, plan []engine.UpgradeItem, dryRun bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, it := range plan {
		ver := it.From
		if it.To != it.From {
			ver += " → " + it.To
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", it.Name, ver, upgradeNote(it, dryRun))
	}
	return tw.Flush()
}

// upgradeReasonText are the reasons in human words.
var upgradeReasonText = map[string]string{
	engine.ReasonRisk:       "риск",
	engine.ReasonPlatform:   "платформа",
	engine.ReasonDependency: "зависимость",
	engine.ReasonHeld:       "закреплено",
	engine.ReasonSource:     "источник",
	engine.ReasonVersion:    "версия",
	engine.ReasonData:       "данные",
	engine.ReasonNoSource:   "не из источника",
	engine.ReasonError:      "ошибка",
}

func upgradeNote(it engine.UpgradeItem, dryRun bool) string {
	retry := it.Action == engine.ActionRetry
	switch it.Status {
	case engine.StatusPlanned:
		if retry {
			return "будет повторено"
		}
		return "будет обновлено"
	case engine.StatusUpgraded:
		if retry {
			return "повторено"
		}
		return "обновлено"
	case engine.StatusUnchanged:
		return "без изменений"
	case engine.StatusHeld:
		return "закреплено — снять: cream unhold " + it.Name
	case engine.StatusNotRun:
		if dryRun {
			return "не будет выполнено (--fail-fast)"
		}
		return "не выполнено (--fail-fast)"
	case engine.StatusFailed:
		return "ошибка: " + it.Error.Message
	}
	note := "пропущено (" + upgradeReasonText[it.Reason] + ")"
	if it.Error != nil {
		note += ": " + it.Error.Message
	}
	return note
}

// upgradeSummaryLine is «Итого: обновлено N, без изменений M, пропущено K
// (причина: имена; …), ошибок E»; with --dry-run «План: будет обновлено …».
func upgradeSummaryLine(plan []engine.UpgradeItem, s engine.UpgradeSummary, dryRun bool) string {
	head, upgraded := "Итого: обновлено", s.Upgraded
	if dryRun {
		head, upgraded = "План: будет обновлено", 0
		for _, it := range plan {
			if it.Status == engine.StatusPlanned {
				upgraded++
			}
		}
	}
	var order []string
	byReason := map[string][]string{}
	for _, it := range plan {
		var why string
		switch it.Status {
		case engine.StatusHeld, engine.StatusSkipped, engine.StatusBlocked:
			why = upgradeReasonText[it.Reason]
		case engine.StatusNotRun:
			why = "не выполнено"
		default:
			continue
		}
		if _, ok := byReason[why]; !ok {
			order = append(order, why)
		}
		byReason[why] = append(byReason[why], it.Name)
	}
	skipped := fmt.Sprintf("пропущено %d", s.Skipped)
	if len(order) > 0 {
		parts := make([]string, 0, len(order))
		for _, why := range order {
			parts = append(parts, why+": "+strings.Join(byReason[why], ", "))
		}
		skipped += " (" + strings.Join(parts, "; ") + ")"
	}
	return fmt.Sprintf("%s %d, без изменений %d, %s, ошибок %d", head, upgraded, s.Unchanged, skipped, s.Failed)
}

// holdCommand is `cream hold <ext>…` (held true) or `cream unhold <ext>…`.
func holdCommand(name string, held bool) Command {
	usage := "hold <ext>…  — закрепить версию: upgrade --all пропускает, upgrade <ext> отказывает"
	if !held {
		usage = "unhold <ext>…  — снять закрепление cream hold"
	}
	return Command{Name: name, Usage: usage,
		Run: func(ctx *Context, args []string) (any, error) {
			if len(args) == 0 {
				return nil, ctx.Usagef("%s: нужны имена расширений", name)
			}
			if err := requireRoot(ctx, name); err != nil {
				return nil, err
			}
			res, err := newEngine(ctx).Hold(ctx.Ctx, args, held)
			if err != nil {
				return nil, err
			}
			return holdResult{Extensions: res}, nil
		}}
}

// holdResult is the result of hold/unhold (golden hold.json, unhold.json).
type holdResult struct {
	Extensions []engine.HoldResult `json:"extensions"`
}

func (r holdResult) WriteText(w io.Writer) error {
	for _, x := range r.Extensions {
		var note string
		switch {
		case x.Held && x.Changed:
			note = "закреплено"
		case x.Held:
			note = "уже закреплено"
		case x.Changed:
			note = "закрепление снято"
		default:
			note = "не было закреплено"
		}
		if _, err := fmt.Fprintf(w, "%s %s: %s\n", x.Name, x.Version, note); err != nil {
			return err
		}
	}
	return nil
}
