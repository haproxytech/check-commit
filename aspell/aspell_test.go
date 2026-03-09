package aspell

import (
	"testing"

	"github.com/haproxytech/check-commit/v5/junit"
)

func Test_checkWithAspell(t *testing.T) {
	aspell := Aspell{
		Mode:         modeSubject,
		MinLength:    3,
		AllowedWords: []string{"config"},
	}
	tests := []struct {
		name    string
		subject string
		wantErr bool
	}{
		{"OK 1", "BUG/MEDIUM: config: add default location of path to the configuration file", false},
		{"OK 2", "BUG/MEDIUM: config: add default location of path to the configuration file xtra", false},
		{"error - flie", "BUG/MEDIUM: config: add default location of path to the configuration flie", true},
		{"error - locatoin", "CLEANUP/MEDIUM: config: add default locatoin of path to the configuration file", true},
		{"error - locatoin+flie", "CLEANUP/MEDIUM: config: add default locatoin of path to the configuration flie", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := aspell.checkSingle(tt.subject, []string{"xtra"})
			if tt.wantErr && err == nil {
				t.Errorf("checkWithAspell() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("checkWithAspell() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_removeKnownHashesFromBody(t *testing.T) {
	hashes := map[string]struct{}{
		"abc1234567890abcdef1234567890abcdef12340": {},
		"def9876543210fedcba9876543210fedcba98760": {},
	}
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			"no body",
			"subject line only",
			"subject line only",
		},
		{
			"hash in body is removed",
			"subject line\n\nThis reverts abc1234567890abcdef1234567890abcdef12340.",
			"subject line\n\nThis reverts .",
		},
		{
			"short hash in body is removed",
			"subject line\n\nThis reverts abc1234.",
			"subject line\n\nThis reverts .",
		},
		{
			"hash in subject is preserved",
			"revert abc1234\n\nbody text here",
			"revert abc1234\n\nbody text here",
		},
		{
			"unknown hex string preserved",
			"subject\n\nSee fffffff for details",
			"subject\n\nSee fffffff for details",
		},
		{
			"multiple hashes in body",
			"subject\n\nReverts abc1234 and def9876.",
			"subject\n\nReverts  and .",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeKnownHashesFromBody(tt.input, hashes)
			if got != tt.expect {
				t.Errorf("removeKnownHashesFromBody() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestAspell_Check(t *testing.T) {
	type fields struct {
		Mode         mode
		MinLength    int
		IgnoreFiles  []string
		AllowedWords []string
		HelpText     string
	}
	type args struct {
		subjects    []string
		commitsFull []string
		content     []map[string]string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{{
		"Signed off",
		fields{
			Mode:         modeCommit,
			MinLength:    3,
			IgnoreFiles:  []string{"config"},
			AllowedWords: []string{"config"},
			HelpText:     "test",
		},
		args{
			subjects:    []string{"BUG/MEDIUM: config: add default location of path to the configuration file"},
			commitsFull: []string{"   Signed-off-by: Author: A locatoin <al@al.al>"},
			content:     []map[string]string{{"test": "test"}},
		},
		false,
	}, {
		"Signed off",
		fields{
			Mode:         modeCommit,
			MinLength:    3,
			IgnoreFiles:  []string{"config"},
			AllowedWords: []string{"config"},
			HelpText:     "test",
		},
		args{
			subjects:    []string{"BUG/MEDIUM: config: add default location of path to the configuration file"},
			commitsFull: []string{"mitsake", "   Signed-off-by: Author: A locatoin <al@al.al>"},
			content:     []map[string]string{{"test": "test"}},
		},
		true,
	}, {
		"Signed off 2",
		fields{
			Mode:         modeCommit,
			MinLength:    3,
			IgnoreFiles:  []string{"config"},
			AllowedWords: []string{"config"},
			HelpText:     "test",
		},
		args{
			subjects:    []string{"BUG/MEDIUM: config: add default location of path to the configuration file"},
			commitsFull: []string{"some commit info\n\n   Signed-off-by: Author: A locatoin <al@al.al>"},
			content:     []map[string]string{{"test": "test"}},
		},
		false,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Aspell{
				Mode:         tt.fields.Mode,
				MinLength:    tt.fields.MinLength,
				IgnoreFiles:  tt.fields.IgnoreFiles,
				AllowedWords: tt.fields.AllowedWords,
				HelpText:     tt.fields.HelpText,
			}
			if err := a.Check(tt.args.subjects, tt.args.commitsFull, tt.args.content, &junit.JunitSuiteDummy{}, nil); (err != nil) != tt.wantErr {
				t.Errorf("Aspell.Check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
