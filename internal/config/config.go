package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const DefaultFilename = "seeder.yaml"

type Config struct {
	Seed     *uint64                `yaml:"seed,omitempty"`
	Rows     *int                   `yaml:"rows,omitempty"`
	Locale   string                 `yaml:"locale,omitempty"`
	Truncate *bool                  `yaml:"truncate,omitempty"`
	Tables   map[string]TableConfig `yaml:"tables,omitempty"`
}

type TableConfig struct {
	Rows        *int                    `yaml:"rows,omitempty"`
	Exclude     bool                    `yaml:"exclude,omitempty"`
	Columns     map[string]ColumnConfig `yaml:"columns,omitempty"`
	Polymorphic []PolymorphicConfig     `yaml:"polymorphic,omitempty"`
}

// ColumnConfig requires exactly one of Generator or Value (see validateColumn).
type ColumnConfig struct {
	Generator string `yaml:"generator,omitempty"`
	Value     any    `yaml:"value,omitempty"`
	Exclude   bool   `yaml:"exclude,omitempty"`
}

// PolymorphicConfig declares a Rails-style polymorphic association: TypeColumn
// stores the target table's discriminator (Target.Type), IDColumn stores the
// target row's id. Introspection cannot detect this on its own; users must
// list each polymorphic site explicitly.
type PolymorphicConfig struct {
	TypeColumn string              `yaml:"type_col"`
	IDColumn   string              `yaml:"id_col"`
	Targets    []PolymorphicTarget `yaml:"targets"`
}

// PolymorphicTarget points IDColumn at a row in Table and writes Type into
// TypeColumn. IDCol picks which column of Table to read; empty defaults to
// the target table's first primary-key column.
type PolymorphicTarget struct {
	Table string `yaml:"table"`
	Type  string `yaml:"type"`
	IDCol string `yaml:"id_col,omitempty"`
}

// Load returns an error wrapping os.ErrNotExist when the file is missing
// so callers can use errors.Is to treat absence as a non-error.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is user-supplied via --config by design
	if err != nil {
		return Config{}, fmt.Errorf("seeder.yaml: read %s: %w", path, err)
	}

	return Parse(data)
}

func Parse(data []byte) (Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var c Config
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("seeder.yaml: %w", err)
	}

	if c.Rows != nil && *c.Rows < 0 {
		return Config{}, fmt.Errorf("seeder.yaml: rows must be >= 0, got %d", *c.Rows)
	}
	for name, t := range c.Tables {
		if t.Rows != nil && *t.Rows < 0 {
			return Config{}, fmt.Errorf("seeder.yaml: tables.%s.rows must be >= 0, got %d", name, *t.Rows)
		}
		for col, cc := range t.Columns {
			if err := validateColumn(name, col, cc); err != nil {
				return Config{}, err
			}
		}
		seenPolyCols := make(map[string]string)
		for i, p := range t.Polymorphic {
			if err := validatePolymorphic(name, i, p); err != nil {
				return Config{}, err
			}
			for _, ref := range []struct{ col, kind string }{
				{p.TypeColumn, "type_col"},
				{p.IDColumn, "id_col"},
			} {
				if where, dup := seenPolyCols[ref.col]; dup {
					return Config{}, fmt.Errorf(
						"seeder.yaml: tables.%s.polymorphic[%d].%s: column %q already used by %s",
						name, i, ref.kind, ref.col, where,
					)
				}
				seenPolyCols[ref.col] = fmt.Sprintf("polymorphic[%d].%s", i, ref.kind)
			}
		}
	}

	return c, nil
}

func validateColumn(table, col string, cc ColumnConfig) error {
	if cc.Exclude {
		return nil
	}
	hasGen := cc.Generator != ""
	hasVal := cc.Value != nil
	switch {
	case hasGen && hasVal:
		return fmt.Errorf("seeder.yaml: tables.%s.columns.%s: cannot set both `generator` and `value`", table, col)
	case !hasGen && !hasVal:
		return fmt.Errorf("seeder.yaml: tables.%s.columns.%s: one of `generator` or `value` must be set", table, col)
	}
	if hasVal {
		switch cc.Value.(type) {
		case string, bool, int, int64, uint64, float64:
		default:
			return fmt.Errorf(
				"seeder.yaml: tables.%s.columns.%s.value must be a scalar (string, number, bool), got %T",
				table, col, cc.Value,
			)
		}
	}
	return nil
}

func validatePolymorphic(table string, idx int, p PolymorphicConfig) error {
	if p.TypeColumn == "" {
		return fmt.Errorf("seeder.yaml: tables.%s.polymorphic[%d].type_col is required", table, idx)
	}
	if p.IDColumn == "" {
		return fmt.Errorf("seeder.yaml: tables.%s.polymorphic[%d].id_col is required", table, idx)
	}
	if p.TypeColumn == p.IDColumn {
		return fmt.Errorf("seeder.yaml: tables.%s.polymorphic[%d]: type_col and id_col must differ", table, idx)
	}
	if len(p.Targets) == 0 {
		return fmt.Errorf("seeder.yaml: tables.%s.polymorphic[%d].targets is required", table, idx)
	}
	for j, target := range p.Targets {
		if target.Table == "" {
			return fmt.Errorf("seeder.yaml: tables.%s.polymorphic[%d].targets[%d].table is required", table, idx, j)
		}
		if target.Type == "" {
			return fmt.Errorf("seeder.yaml: tables.%s.polymorphic[%d].targets[%d].type is required", table, idx, j)
		}
	}
	return nil
}

// AutoDetect returns found=false (without error) when DefaultFilename is
// absent in dir, so the CLI can silently fall back to flag defaults.
func AutoDetect(dir string) (cfg Config, found bool, err error) {
	cfg, err = Load(filepath.Join(dir, DefaultFilename))
	switch {
	case err == nil:
		return cfg, true, nil
	case errors.Is(err, os.ErrNotExist):
		return Config{}, false, nil
	default:
		return Config{}, false, err
	}
}
