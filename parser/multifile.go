package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverFiles resolves a list of paths into an ordered list of SQL files
// suitable for parsing. Each path may be a file or a directory.
// Directories are expanded to their contained .sql files in lexicographic order.
// Down-migration files (*.down.sql, *_down.sql) are automatically excluded.
// Paths are processed in the order specified; ordering within a directory
// is lexicographic.
func DiscoverFiles(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("discovering files: stat %s: %w", p, err)
		}
		if info.IsDir() {
			dirFiles, err := discoverDir(p)
			if err != nil {
				return nil, err
			}
			files = append(files, dirFiles...)
		} else if !isDownMigration(info.Name()) {
			files = append(files, p)
		}
	}
	return files, nil
}

func discoverDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}
	// os.ReadDir returns entries sorted by name (lexicographic).
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		if isDownMigration(name) {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	return files, nil
}

// isDownMigration returns true if the filename matches a down-migration pattern.
func isDownMigration(name string) bool {
	return strings.HasSuffix(name, ".down.sql") || strings.HasSuffix(name, "_down.sql")
}

// ParseFiles parses multiple SQL files sequentially, applying each file's
// statements to the parser's accumulating schema. Files are processed in the
// order provided (use DiscoverFiles to resolve paths).
//
// Each file's DDL is applied as a natural SQL migration: ALTER TABLE extends
// or modifies tables, DROP TABLE removes tables, and so on. Duplicate CREATE
// statements (table, enum, composite type, domain type) are validation errors
// — each schema element must be defined exactly once.
func ParseFiles(p Parser, files []string) error {
	for _, file := range files {
		sql, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}
		if err := p.Parse(file, sql); err != nil {
			return fmt.Errorf("parsing %s: %w", file, err)
		}
	}
	return nil
}
