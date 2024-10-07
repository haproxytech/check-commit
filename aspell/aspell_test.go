package aspell

import (
	"testing"
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
			if err := a.Check(tt.args.subjects, tt.args.commitsFull, tt.args.content); (err != nil) != tt.wantErr {
				t.Errorf("Aspell.Check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
