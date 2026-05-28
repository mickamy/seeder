package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mickamy/seeder/internal/cli"
	"github.com/mickamy/seeder/internal/config"
	"github.com/mickamy/seeder/internal/infer"
	"github.com/mickamy/seeder/internal/introspect"
)

func TestReorderArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{
			name: "flags then dsn",
			in:   []string{"--rows", "10", "--dry-run", "postgres://x"},
			want: []string{"--rows", "10", "--dry-run", "postgres://x"},
		},
		{
			name: "dsn first",
			in:   []string{"postgres://x", "--rows", "10", "--dry-run"},
			want: []string{"--rows", "10", "--dry-run", "postgres://x"},
		},
		{
			name: "equals form",
			in:   []string{"postgres://x", "--rows=10"},
			want: []string{"--rows=10", "postgres://x"},
		},
		{
			name: "double-dash terminator preserves trailing dashes",
			in:   []string{"--rows", "10", "--", "-weird-dsn-with-dashes"},
			want: []string{"--rows", "10", "-weird-dsn-with-dashes"},
		},
		{
			name: "bool flag is not greedy",
			in:   []string{"--dry-run", "postgres://x"},
			want: []string{"--dry-run", "postgres://x"},
		},
		{
			name:    "value flag missing value",
			in:      []string{"--rows"},
			want:    nil,
			wantErr: true,
		},
		{
			name: "negative seed value",
			in:   []string{"postgres://x", "--seed", "-42"},
			want: []string{"--seed", "-42", "postgres://x"},
		},
		{
			name: "locale flag with dsn first",
			in:   []string{"postgres://x", "--locale", "ja"},
			want: []string{"--locale", "ja", "postgres://x"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := cli.ReorderArgs(tc.in, cli.ValueFlags)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil; out=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v; want %v", got, tc.want)
			}
		})
	}
}

func TestSplitTrim(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want []string
	}{
		{"users,orders", []string{"users", "orders"}},
		{"users, orders, comments", []string{"users", "orders", "comments"}},
		{"users,,orders", []string{"users", "orders"}},
		{"  ", []string{}},
		{"", []string{}},
	}
	for _, tc := range cases {
		got := cli.SplitTrim(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitTrim(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestIncludeTables(t *testing.T) {
	t.Parallel()

	tables := []introspect.Table{{Name: "users"}, {Name: "orders"}, {Name: "comments"}}

	got, missing := cli.IncludeTables(tables, []string{"users", "comments"})
	if len(missing) != 0 {
		t.Errorf("missing = %v; want []", missing)
	}
	gotNames := tableNames(got)
	wantNames := []string{"users", "comments"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("got %v; want %v (input order preserved)", gotNames, wantNames)
	}

	_, missing = cli.IncludeTables(tables, []string{"users", "bogus"})
	if !slices.Contains(missing, "bogus") {
		t.Errorf("missing = %v; want bogus included", missing)
	}
}

func TestExcludeColumns(t *testing.T) {
	t.Parallel()

	usersColumns := []introspect.Column{
		{
			Name: "user_col_1",
		},
		{
			Name: "user_col_2",
		},
		{
			Name: "user_pk",
		},
	}
	ordersColumns := []introspect.Column{
		{
			Name: "order_col_1",
		},
		{
			Name: "order_col_2",
		},
		{
			Name: "order_fk",
		},
	}
	ordersFk := []introspect.ForeignKey{
		{
			Columns: []string{"order_fk"},
		},
	}
	tables := []introspect.Table{
		{Name: "users", Columns: usersColumns, PrimaryKey: []string{"user_pk"}},
		{Name: "orders", Columns: ordersColumns, ForeignKeys: ordersFk},
	}
	schema := introspect.Schema{Tables: tables}
	cfg := config.Config{
		Tables: map[string]config.TableConfig{
			"users": {Columns: map[string]config.ColumnConfig{
				"user_col_2": {
					Exclude: true,
				},
				"user_pk": {
					Exclude: true,
				},
			}},
			"orders": {Columns: map[string]config.ColumnConfig{
				"order_fk": {
					Exclude: true,
				},
			}},
		},
	}

	got := cli.ExcludeColumns(schema, cfg)
	for _, table := range got.Tables {
		if table.Name == "users" {
			if len(table.Columns) != 2 {
				t.Errorf("got wrong amount of columns for 'users'-table, got: %d, want: 2", len(table.Columns))
			}
			columnNames := []string{table.Columns[0].Name, table.Columns[1].Name}
			if slices.Contains(columnNames, "user_col_2") {
				t.Errorf(`got wrong columns, got: %+v, want: [user_col_1, user_pk]`, columnNames)
			}
		} else if table.Name == "orders" && len(table.Columns) != 3 {
			t.Errorf("got wrong amount of columns for 'orders'-table, got: %d, want: 3", len(table.Columns))
		}
	}
}

func TestExcludeTables(t *testing.T) {
	t.Parallel()

	tables := []introspect.Table{{Name: "users"}, {Name: "orders"}, {Name: "comments"}}

	got, missing := cli.ExcludeTables(tables, []string{"orders"})
	if len(missing) != 0 {
		t.Errorf("missing = %v; want []", missing)
	}
	gotNames := tableNames(got)
	wantNames := []string{"users", "comments"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("got %v; want %v", gotNames, wantNames)
	}

	_, missing = cli.ExcludeTables(tables, []string{"bogus"})
	if !slices.Contains(missing, "bogus") {
		t.Errorf("missing = %v; want bogus included", missing)
	}
}

func TestOrphanFKs_MissingParentNotNull(t *testing.T) {
	t.Parallel()

	tables := []introspect.Table{ordersTable(false)}
	got := cli.OrphanFKs(tables)
	if len(got) != 1 {
		t.Fatalf("orphan count = %d; want 1", len(got))
	}
	if got[0].FromTable != "orders" || got[0].FromCol != "user_id" || got[0].ToTable != "users" {
		t.Errorf("orphan = %+v; want orders.user_id -> users.id", got[0])
	}
}

func TestOrphanFKs_NullableIgnored(t *testing.T) {
	t.Parallel()

	tables := []introspect.Table{ordersTable(true)}
	if got := cli.OrphanFKs(tables); len(got) != 0 {
		t.Errorf("nullable orphan should be ignored; got %v", got)
	}
}

func TestOrphanFKs_ParentPresent(t *testing.T) {
	t.Parallel()

	tables := []introspect.Table{ordersTable(false), {Name: "users"}}
	if got := cli.OrphanFKs(tables); len(got) != 0 {
		t.Errorf("with parent present orphan count = %d; want 0", len(got))
	}
}

func TestRun_MutuallyExclusiveFilters(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	code := cli.Run([]string{"postgres://x", "--tables", "a", "--exclude", "b"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d; want 2", code)
	}
	if !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Errorf("stderr = %q; want mention of mutual exclusion", stderr.String())
	}
}

func TestRun_MissingDSN(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	code := cli.Run(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d; want 2 (Usage)", code)
	}
	if !strings.Contains(stderr.String(), "missing <dsn>") {
		t.Errorf("stderr = %q; want missing dsn message", stderr.String())
	}
}

func TestRun_NegativeSeed(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	code := cli.Run([]string{"postgres://x", "--seed", "-1"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d; want 2 (Usage)", code)
	}
	if !strings.Contains(stderr.String(), "--seed must be >= 0") {
		t.Errorf("stderr = %q; want negative seed message", stderr.String())
	}
}

func TestRun_NegativeRows(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	code := cli.Run([]string{"postgres://x", "--rows", "-1"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d; want 2 (Usage)", code)
	}
	if !strings.Contains(stderr.String(), "--rows must be >= 0") {
		t.Errorf("stderr = %q; want negative rows message", stderr.String())
	}
}

func TestApplyTableFilters_YamlExclude(t *testing.T) {
	t.Parallel()

	schema := introspect.Schema{Tables: []introspect.Table{
		{Name: "users"}, {Name: "orders"}, {Name: "audit_log"},
	}}
	cfg := config.Config{
		Tables: map[string]config.TableConfig{
			"audit_log": {Exclude: true},
		},
	}

	schema, missing := cli.ApplyTableFilters(schema, "", "", cfg)
	if len(missing) != 0 {
		t.Errorf("missing = %v; want []", missing)
	}
	got := tableNames(schema.Tables)
	want := []string{"users", "orders"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tables = %v; want %v", got, want)
	}
}

func TestApplyTableFilters_CLIWinsOverYaml(t *testing.T) {
	t.Parallel()

	schema := introspect.Schema{Tables: []introspect.Table{
		{Name: "users"}, {Name: "orders"}, {Name: "audit_log"},
	}}
	cfg := config.Config{
		Tables: map[string]config.TableConfig{
			"audit_log": {Exclude: true},
		},
	}

	schema, _ = cli.ApplyTableFilters(schema, "users,audit_log", "", cfg)
	got := tableNames(schema.Tables)
	want := []string{"users", "audit_log"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tables = %v; want %v (CLI --tables wins; yaml exclude ignored)", got, want)
	}
}

func TestBuildInsertOptions_RowsPriority(t *testing.T) {
	t.Parallel()

	five := 5
	twenty := 20
	cfg := config.Config{
		Rows: &five,
		Tables: map[string]config.TableConfig{
			"users": {Rows: &twenty},
		},
	}

	cases := []struct {
		name              string
		rows              int
		set               map[string]bool
		wantDefaultRows   int
		wantUsersOverride int
	}{
		{
			name:              "cli explicit overrides yaml (no per-table override applied)",
			rows:              100,
			set:               map[string]bool{"rows": true},
			wantDefaultRows:   100,
			wantUsersOverride: 0,
		},
		{
			name:              "yaml takes effect when cli omits --rows",
			rows:              1000,
			set:               map[string]bool{},
			wantDefaultRows:   5,
			wantUsersOverride: 20,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := cli.BuildInsertOptions(tc.rows, 1000, false, 0, false, false, "", infer.LocaleEN, tc.set, cfg)
			if opts.Rows != tc.wantDefaultRows {
				t.Errorf("Rows = %d; want %d", opts.Rows, tc.wantDefaultRows)
			}
			if got := opts.RowsByTable["users"]; got != tc.wantUsersOverride {
				t.Errorf("RowsByTable[users] = %d; want %d", got, tc.wantUsersOverride)
			}
		})
	}
}

func TestBuildInsertOptions_SeedPriority(t *testing.T) {
	t.Parallel()

	yamlSeed := uint64(7)
	cfg := config.Config{Seed: &yamlSeed}

	cases := []struct {
		name string
		set  map[string]bool
		seed int64
		want uint64
	}{
		{"cli wins", map[string]bool{"seed": true}, 42, 42},
		{"yaml fallback", map[string]bool{}, 0, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := cli.BuildInsertOptions(1, 1000, false, tc.seed, false, false, "", infer.LocaleEN, tc.set, cfg)
			if opts.Seed == nil {
				t.Fatal("Seed = nil; want non-nil")
			}
			if *opts.Seed != tc.want {
				t.Errorf("Seed = %d; want %d", *opts.Seed, tc.want)
			}
		})
	}
}

func TestBuildInsertOptions_TruncatePriority(t *testing.T) {
	t.Parallel()

	yamlTrue := true
	yamlFalse := false

	cases := []struct {
		name        string
		cliTruncate bool
		set         map[string]bool
		cfg         config.Config
		want        bool
	}{
		{
			name:        "cli explicit true overrides yaml false",
			cliTruncate: true,
			set:         map[string]bool{"truncate": true},
			cfg:         config.Config{Truncate: &yamlFalse},
			want:        true,
		},
		{
			name:        "yaml true takes effect when cli omits --truncate",
			cliTruncate: false,
			set:         map[string]bool{},
			cfg:         config.Config{Truncate: &yamlTrue},
			want:        true,
		},
		{
			name:        "yaml false takes effect when cli omits --truncate",
			cliTruncate: false,
			set:         map[string]bool{},
			cfg:         config.Config{Truncate: &yamlFalse},
			want:        false,
		},
		{
			name:        "default false when both unset",
			cliTruncate: false,
			set:         map[string]bool{},
			cfg:         config.Config{},
			want:        false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := cli.BuildInsertOptions(1, 1000, tc.cliTruncate, 0, false, false, "", infer.LocaleEN, tc.set, tc.cfg)
			if opts.Truncate != tc.want {
				t.Errorf("Truncate = %v; want %v", opts.Truncate, tc.want)
			}
		})
	}
}

func TestBuildInsertOptions_NoConfigNoSeed(t *testing.T) {
	t.Parallel()

	opts := cli.BuildInsertOptions(
		10, 1000, false, 0, false, false, "",
		infer.LocaleEN, map[string]bool{}, config.Config{},
	)
	if opts.Seed != nil {
		t.Errorf("Seed = %v; want nil (time-based)", opts.Seed)
	}
	if opts.Rows != 10 {
		t.Errorf("Rows = %d; want 10", opts.Rows)
	}
}

func TestBuildInsertOptions_LocaleWiresThrough(t *testing.T) {
	t.Parallel()

	opts := cli.BuildInsertOptions(1, 1000, false, 0, false, false, "", infer.LocaleJA, map[string]bool{}, config.Config{})
	if opts.Locale != infer.LocaleJA {
		t.Errorf("Locale = %q; want %q", opts.Locale, infer.LocaleJA)
	}
}

func TestUnknownConfigTables(t *testing.T) {
	t.Parallel()

	five := 5
	cfg := config.Config{
		Tables: map[string]config.TableConfig{
			"users":    {Rows: &five},
			"usres":    {Rows: &five},   // typo
			"audit_lg": {Exclude: true}, // typo
		},
	}
	schema := introspect.Schema{Tables: []introspect.Table{
		{Name: "users"}, {Name: "orders"}, {Name: "audit_log"},
	}}

	got := cli.UnknownConfigTables(cfg, schema)
	want := []string{"audit_lg", "usres"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UnknownConfigTables = %v; want %v", got, want)
	}
}

func TestUnknownConfigTables_AllKnown(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Tables: map[string]config.TableConfig{
			"users":  {Exclude: true},
			"orders": {Exclude: true},
		},
	}
	schema := introspect.Schema{Tables: []introspect.Table{
		{Name: "users"}, {Name: "orders"},
	}}

	if got := cli.UnknownConfigTables(cfg, schema); got != nil {
		t.Errorf("UnknownConfigTables = %v; want nil", got)
	}
}

func TestRun_ConfigErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	unknownField := filepath.Join(dir, "seeder.yaml")
	if err := os.WriteFile(unknownField, []byte("truncates: false\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"missing file", filepath.Join(dir, "does-not-exist.yaml"), "seeder.yaml:"},
		{"unknown field", unknownField, "field truncates not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr strings.Builder
			code := cli.Run([]string{"postgres://x", "--config", tc.path}, &stdout, &stderr)
			if code != 2 {
				t.Errorf("exit code = %d; want 2 (Usage)", code)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q; want substring %q", stderr.String(), tc.want)
			}
		})
	}
}

func TestRun_UnknownLocale(t *testing.T) {
	t.Parallel()

	// Point at a temp config so the ambient cwd seeder.yaml (if any) cannot
	// fail the test for unrelated reasons.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "seeder.yaml")
	if err := os.WriteFile(cfgPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var stdout, stderr strings.Builder
	code := cli.Run([]string{"postgres://x", "--locale", "fr", "--config", cfgPath}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d; want 2 (Usage)", code)
	}
	if !strings.Contains(stderr.String(), "unknown locale") {
		t.Errorf("stderr = %q; want unknown locale message", stderr.String())
	}
}

func TestEffectiveLocaleString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		set    map[string]bool
		cliArg string
		cfg    config.Config
		want   string
	}{
		{"cli wins when set", map[string]bool{"locale": true}, "en", config.Config{Locale: "ja"}, "en"},
		{"yaml fills in when cli omitted", map[string]bool{}, "", config.Config{Locale: "ja"}, "ja"},
		{"empty when both unset", map[string]bool{}, "", config.Config{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cli.EffectiveLocaleString(tc.set, tc.cliArg, tc.cfg); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestValidateColumnGenerators(t *testing.T) {
	t.Parallel()

	ok := config.Config{
		Tables: map[string]config.TableConfig{
			"users": {Columns: map[string]config.ColumnConfig{
				"email": {Generator: "Email"},
			}},
		},
	}
	if msg := cli.ValidateColumnGenerators(ok); msg != "" {
		t.Errorf("ValidateColumnGenerators(ok) = %q; want \"\"", msg)
	}

	bad := config.Config{
		Tables: map[string]config.TableConfig{
			"users": {Columns: map[string]config.ColumnConfig{
				"email": {Generator: "NotAGenerator"},
			}},
		},
	}
	msg := cli.ValidateColumnGenerators(bad)
	if !strings.Contains(msg, "unknown generator") {
		t.Errorf("ValidateColumnGenerators(bad) = %q; want unknown generator message", msg)
	}
	if !strings.Contains(msg, "users.email") {
		t.Errorf("ValidateColumnGenerators(bad) = %q; want users.email reference", msg)
	}
}

func TestUnknownConfigColumns(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Tables: map[string]config.TableConfig{
			"users": {Columns: map[string]config.ColumnConfig{
				"email":  {Generator: "Email"},
				"emial":  {Generator: "Email"}, // typo
				"unknwn": {Value: 1},           // typo
			}},
		},
	}
	schema := introspect.Schema{Tables: []introspect.Table{
		{Name: "users", Columns: []introspect.Column{{Name: "email"}, {Name: "id"}}},
	}}

	got := cli.UnknownConfigColumns(cfg, schema)
	want := []string{"users.emial", "users.unknwn"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UnknownConfigColumns = %v; want %v", got, want)
	}
}

func TestColumnOverrides(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Tables: map[string]config.TableConfig{
			"users": {Columns: map[string]config.ColumnConfig{
				"email":   {Generator: "Email"},
				"country": {Value: "JP"},
			}},
			"audit_log": {Exclude: true}, // no Columns entries
		},
	}

	got := cli.ColumnOverrides(cfg)
	if _, ok := got["audit_log"]; ok {
		t.Errorf("audit_log should not appear in overrides")
	}
	users := got["users"]
	if users["email"].Generator != "Email" {
		t.Errorf("users.email = %+v; want Generator=Email", users["email"])
	}
	if users["country"].Value != "JP" {
		t.Errorf("users.country = %+v; want Value=JP", users["country"])
	}
}

func ordersTable(nullable bool) introspect.Table {
	return introspect.Table{
		Name: "orders",
		Columns: []introspect.Column{
			{Name: "id", Kind: introspect.KindInt},
			{Name: "user_id", Kind: introspect.KindInt, Nullable: nullable},
		},
		ForeignKeys: []introspect.ForeignKey{
			{Name: "fk_user", Columns: []string{"user_id"}, ReferencedTable: "users", ReferencedColumns: []string{"id"}},
		},
	}
}

func tableNames(tables []introspect.Table) []string {
	out := make([]string, 0, len(tables))
	for _, t := range tables {
		out = append(out, t.Name)
	}

	return out
}
