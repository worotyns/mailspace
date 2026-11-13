//go:build ignore

// SPDX-License-Identifier: AGPL-3.0-only

// Generate notices only for Go modules used by the shipped binary.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type module struct {
	Path    string
	Version string
	Dir     string
	Main    bool
}

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func generate() error {
	command := exec.Command("go", "list", "-deps", "-json", "./cmd/mailspace")
	output, err := command.Output()
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	modules := map[string]module{}
	for {
		var pkg struct{ Module *module }
		err = decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if pkg.Module != nil && !pkg.Module.Main {
			modules[pkg.Module.Path] = *pkg.Module
		}
	}
	keys := []string{}
	for path := range modules {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	goLicense, err := os.ReadFile("licenses/Go-LICENSE.txt")
	if err != nil {
		return err
	}
	fmt.Printf("=== Go standard library ===\n%s\n", goLicense)
	for _, path := range keys {
		m := modules[path]
		files, err := os.ReadDir(m.Dir)
		if err != nil {
			return err
		}
		found := false
		for _, file := range files {
			name := strings.ToUpper(file.Name())
			if file.IsDir() || !(strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "COPYING") || strings.HasPrefix(name, "NOTICE") || name == "UNLICENSE") {
				continue
			}
			text, err := os.ReadFile(filepath.Join(m.Dir, file.Name()))
			if err != nil {
				return err
			}
			found = true
			fmt.Printf("\n=== %s %s / %s ===\n%s\n", m.Path, m.Version, file.Name(), text)
		}
		if !found {
			return fmt.Errorf("missing upstream license notice for %s %s", m.Path, m.Version)
		}
	}
	return nil
}
