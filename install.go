package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed skill/SKILL.md
var skillTemplate string

const skillSubdir = "skills/csm"
const skillFile = "SKILL.md"

// installSkill writes the embedded SKILL.md to ~/.claude/skills/csm/SKILL.md.
// If a file already exists at that path, returns an error unless force is true.
func installSkill(force bool) error {
	dir, file, err := skillPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(file); err == nil && !force {
		return fmt.Errorf("skill already installed at %s\n  use 'csm install --force' to overwrite", file)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	if err := os.WriteFile(file, []byte(skillTemplate), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	fmt.Printf("✓ skill installed at %s\n", file)
	fmt.Println("  trigger: '/csm' or natural language ('세션 바꿔', 'switch session', etc.)")
	return nil
}

// uninstallSkill removes the skill file (and its directory if empty).
func uninstallSkill() error {
	dir, file, err := skillPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(file); os.IsNotExist(err) {
		fmt.Printf("skill not installed at %s\n", file)
		return nil
	}

	if err := os.Remove(file); err != nil {
		return fmt.Errorf("remove file: %w", err)
	}
	// Try to remove directory if empty; ignore error if not empty.
	_ = os.Remove(dir)

	fmt.Printf("✓ skill removed from %s\n", file)
	return nil
}

func skillPath() (dir, file string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir = filepath.Join(home, ".claude", skillSubdir)
	file = filepath.Join(dir, skillFile)
	return dir, file, nil
}
