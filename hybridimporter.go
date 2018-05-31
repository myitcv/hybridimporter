package hybridimporter // import "myitcv.io/hybridimporter"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/build"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"

	"myitcv.io/hybridimporter/srcimporter"
)

type pkgInfo struct {
	ImportPath string
	Target     string
	Stale      bool
	Name       string
}

// New returns a go/types.ImporterFrom that uses installed package files if they
// are non-Stale, dropping back to a src-based importer otherwise.
func New(ctxt *build.Context, fset *token.FileSet, path, dir string) (*srcimporter.Importer, error) {
	return newImpl(ctxt, fset, path, dir, false)
}

// NewInstaller returns a go/types.ImporterFrom that behaves identifically to
// the ImporterFrom returned by New, except that it forks a go install of any
// stale packages that are imported along the way (we don't care whether this
// install fails or not). Notice too that we are not waiting _at all_ for the
// install process to finish.... this could be problematic if the calling process
// exits before the install finishes... let's deal with that when we have such
// a use case.
func NewInstaller(ctxt *build.Context, fset *token.FileSet, path, dir string) (*srcimporter.Importer, error) {
	return newImpl(ctxt, fset, path, dir, true)
}

func newImpl(ctxt *build.Context, fset *token.FileSet, path, dir string, install bool) (*srcimporter.Importer, error) {
	cmd := exec.Command("go", "list", "-deps", "-test", "-json", path)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to start list for %v in %v: %v\n%v", path, dir, err, string(out))
	}

	lookups := make(map[string]io.ReadCloser)

	dec := json.NewDecoder(bytes.NewReader(out))

	for {
		var p pkgInfo
		err := dec.Decode(&p)
		if err != nil {
			if io.EOF == err {
				break
			}
			return nil, fmt.Errorf("failed to parse list for %v in %v: %v", path, dir, err)
		}
		if p.ImportPath == "unsafe" || p.Stale || p.Name == "main" {
			continue
		}
		fi, err := os.Open(p.Target)
		if err != nil {
			return nil, fmt.Errorf("failed to open %v: %v", p.Target, err)
		}
		lookups[p.ImportPath] = fi
	}

	lookup := func(path string) (io.ReadCloser, error) {
		rc, ok := lookups[path]
		if !ok {
			return nil, fmt.Errorf("failed to resolve %v", path)
		}

		return rc, nil
	}

	gc := importer.For("gc", lookup)

	tpkgs := make(map[string]*types.Package)

	for path := range lookups {
		p, err := gc.Import(path)
		if err != nil {
			return nil, fmt.Errorf("failed to gc import %v: %v", path, err)
		}
		tpkgs[path] = p
	}

	return srcimporter.New(ctxt, fset, tpkgs, install), nil
}
