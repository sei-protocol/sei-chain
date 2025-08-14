package gomod

import (
	"strings"

	"github.com/sirkon/goproxy/internal/errors"

	"github.com/sirkon/goproxy/internal/modfile"
	"github.com/sirkon/goproxy/internal/modload"
	"github.com/sirkon/goproxy/internal/module"
)

// Replacement is type safe hack to deal with the lack of algebraic/variative typing in Go
type Replacement interface {
	notYourConcern()
}

var _ Replacement = Dependency{}

// Dependency describes module and its version
type Dependency struct {
	Path    string
	Version string
}

func (d Dependency) notYourConcern() {}

var _ Replacement = RelativePath("")

// RelativePath describes relative path replacement
type RelativePath string

func (p RelativePath) notYourConcern() {}

// Module go.mod description
type Module struct {
	Name      string
	GoVersion string

	Require map[string]string
	Exclude map[string]string
	Replace map[string]Replacement
}

// Parse parse input Go file
func Parse(fileName string, input []byte) (*Module, error) {
	gomod, err := modfile.Parse(fileName, input, fixVersion)
	if err != nil {
		return nil, err
	}

	var goVersion string
	if gomod.Go != nil {
		goVersion = gomod.Go.Version
	}
	res := &Module{
		Name:      gomod.Module.Mod.Path,
		GoVersion: goVersion,

		Require: map[string]string{},
		Exclude: map[string]string{},
		Replace: map[string]Replacement{},
	}

	for _, req := range gomod.Require {
		res.Require[req.Mod.Path] = req.Mod.Version
	}
	for _, exc := range gomod.Exclude {
		res.Exclude[exc.Mod.Path] = exc.Mod.Version
	}
	for _, rep := range gomod.Replace {
		if len(rep.New.Version) == 0 {
			// it is path replacement
			res.Replace[rep.Old.Path] = RelativePath(rep.New.Path)
		} else {
			res.Replace[rep.Old.Path] = Dependency{
				Path:    rep.New.Path,
				Version: rep.New.Version,
			}
		}
	}

	return res, nil
}

func fixVersion(path, vers string) (string, error) {
	// Special case: remove the old -gopkgin- hack.
	if strings.HasPrefix(path, "gopkg.in/") && strings.Contains(vers, "-gopkgin-") {
		vers = vers[strings.Index(vers, "-gopkgin-")+len("-gopkgin-"):]
	}

	// fixVersion is called speculatively on every
	// module, version pair from every go.mod file.
	// Avoid the query if it looks OK.
	_, pathMajor, ok := module.SplitPathVersion(path)
	if !ok {
		return "", errors.Newf("malformed module path: %s", path)
	}
	if vers != "" && module.CanonicalVersion(vers) == vers && module.MatchPathMajor(vers, pathMajor) {
		return vers, nil
	}

	info, err := modload.Query(path, vers, nil)
	if err != nil {
		return "", err
	}
	return info.Version, nil
}
