package ipmtmeta

import (
	"reflect"
	"testing"
)

func TestMetaFromContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
		wantErr bool
	}{
		{"no pragma", "A --> B", nil, false},
		{"bare comment not a pragma", "# just a note\nA --> B", nil, false},
		{"ipmt without colon is not a pragma", "# ipmt is nice\nA --> B", nil, false},
		{"single flag", "# ipmt: unresolved\ndeploy ::?etc", []string{"unresolved"}, false},
		{"two flags", "# ipmt: unresolved defaults\nx", []string{"unresolved", "defaults"}, false},
		{"embed=false", "# ipmt: embed=false\nA --> B", []string{"embed=false"}, false},
		{"leading blank lines ok", "\n\n# ipmt: defaults\nx", []string{"defaults"}, false},
		{"tight hash", "#ipmt:defaults\nx", []string{"defaults"}, false},
		{"misplaced pragma errors", "# a comment\n# ipmt: unresolved\nA --> B", nil, true},
		{"pragma after content errors", "A --> B\n# ipmt: unresolved", nil, true},
		{"reserved draft flag errors until it ships", "# ipmt: draft\nu_s --- o_f", nil, true},
		{"unknown flag errors", "# ipmt: unresolvd\nx", nil, true},
		{"embed=true unknown errors", "# ipmt: embed=true\nx", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := MetaFromContent(c.content)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if !c.wantErr && !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestEffectiveMeta(t *testing.T) {
	cases := []struct {
		name    string
		fence   []string
		content string
		want    []string
		wantErr bool
	}{
		{"fence only", []string{"unresolved"}, "A --> B", []string{"unresolved"}, false},
		{"pragma only", nil, "# ipmt: defaults\nx", []string{"defaults"}, false},
		{"union dedup", []string{"unresolved"}, "# ipmt: unresolved defaults\nx", []string{"unresolved", "defaults"}, false},
		{"fence order then pragma", []string{"defaults"}, "# ipmt: unresolved\nx", []string{"defaults", "unresolved"}, false},
		{"unknown fence token errors", []string{"bogus"}, "A --> B", nil, true},
		{"pragma error propagates", nil, "x\n# ipmt: unresolved", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := EffectiveMeta(c.fence, c.content)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if !c.wantErr && !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
