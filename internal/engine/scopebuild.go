package engine

import (
	"maps"
	"slices"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/manifest"
)

// refuseScopeBuild — временный запрет: манифест с ключом scope: build уже
// разбирается (ADR-0004), но движок ещё не умеет держать такое значение только
// в окружении хуков — домен сохранил бы его в config или secrets на машине.
// До поддержки области build (Task 10 плана bisquite-bundled-cream; она снимает
// запрет) install, upgrade и set такого расширения — 64 scope.build_unsupported.
func refuseScopeBuild(m *manifest.Manifest) error {
	if m == nil {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(m.Config)) {
		if m.Config[name].Scope == keytype.ScopeBuild {
			e := cerr.New(cerr.ExitUsage, "scope.build_unsupported",
				"%s: ключ %s со scope: build — эта сборка cream ещё не поддерживает ключи только для сборки (значение сохранилось бы на машине)",
				m.Name, name)
			e.Details = map[string]any{"ext": m.Name, "key": name}
			return e
		}
	}
	return nil
}
