package patchbin

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func TestParsePatchsetWithCover(t *testing.T) {
	file, err := os.Open("fixtures/with-cover.patch")
	defer func() {
		_ = file.Close()
	}()
	if err != nil {
		t.Fatal(err.Error())
	}
	actual, err := ParsePatchset(file)
	if err != nil {
		t.Fatal(err.Error())
	}
	expected := []*Patch{
		{Title: "Add torch deps"},
		{Title: "feat: lets build an rnn"},
		{Title: "chore: add torch to requirements"},
	}
	if len(actual) != len(expected) {
		t.Fatalf("patches not same length (expected:%d, actual:%d)\n", len(expected), len(actual))
	}
	for idx, act := range actual {
		exp := expected[idx]
		if exp.Title != act.Title {
			t.Fatalf("title does not match expected (expected:%s, actual:%s)", exp.Title, act.Title)
		}
	}
}

func TestParsePatchsetEmptyInput(t *testing.T) {
	_, err := ParsePatchset(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty patchset input, got nil")
	}
}

func TestParsePatchsetWhitespaceOnlyInput(t *testing.T) {
	_, err := ParsePatchset(strings.NewReader("   \n\n\t\n"))
	if err == nil {
		t.Fatal("expected error for whitespace-only patchset input, got nil")
	}
}

func TestParsePatchsetGarbageInput(t *testing.T) {
	_, err := ParsePatchset(strings.NewReader("this is not a patch\njust some random text\n"))
	if err == nil {
		t.Fatal("expected error for garbage patchset input, got nil")
	}
}

func TestPatchToDiff(t *testing.T) {
	file, err := os.Open("fixtures/single.patch")
	defer func() {
		_ = file.Close()
	}()
	if err != nil {
		t.Fatal(err.Error())
	}

	fileExp, err := os.Open("fixtures/single.diff")
	defer func() {
		_ = file.Close()
	}()
	if err != nil {
		t.Fatal(err.Error())
	}

	actual, err := patchToDiff(file)
	if err != nil {
		t.Fatal(err.Error())
	}

	by, err := io.ReadAll(fileExp)
	if err != nil {
		t.Fatal("cannot read expected file")
	}

	if actual != string(by) {
		fmt.Println(actual)
		t.Fatal("diff does not match expected")
	}
}

func TestParseID(t *testing.T) {
	tests := []struct {
		input    string
		expected ParsedID
		wantErr  bool
	}{
		{input: "pr-1", expected: ParsedID{PrID: 1, Rev: 0}},
		{input: "1", expected: ParsedID{PrID: 1, Rev: 0}},
		{input: "pr-12.3", expected: ParsedID{PrID: 12, Rev: 3}},
		{input: "12.3", expected: ParsedID{PrID: 12, Rev: 3}},
		{input: "pr-5.v2", expected: ParsedID{PrID: 5, Rev: 2}},
		{input: "invalid", wantErr: true},
		{input: "", wantErr: true},
	}

	for _, tt := range tests {
		got, err := ParseID(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseID(%q) expected error, got nil", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseID(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("ParseID(%q) = %+v, expected %+v", tt.input, got, tt.expected)
			}
		}
	}
}

func TestGetFormattedPatchsetID(t *testing.T) {
	if got := getFormattedPatchsetID(1, 2); got != "1.2" {
		t.Errorf("expected 1.2, got %q", got)
	}
	if got := getFormattedPatchsetID(0, 2); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		input    string
		expected Target
		wantErr  bool
	}{
		{input: "pico:feat/login", expected: Target{Repo: "pico", Slug: "feat/login", Rev: 0}},
		{input: "pico:feat/login.2", expected: Target{Repo: "pico", Slug: "feat/login", Rev: 2}},
		{input: "pico:feat/login.v3", expected: Target{Repo: "pico", Slug: "feat/login", Rev: 3}},
		{input: "pico:feat/login.patch", expected: Target{Repo: "pico", Slug: "feat/login", Rev: 0}},
		{input: "pico:feat/login.2.patch", expected: Target{Repo: "pico", Slug: "feat/login", Rev: 2}},
		{input: "my-repo:123", expected: Target{Repo: "my-repo", Slug: "123", Rev: 0}},
		{input: "my-repo:123.4", expected: Target{Repo: "my-repo", Slug: "123", Rev: 4}},
		{input: "no-colon", wantErr: true},
		{input: ":no-repo", wantErr: true},
		{input: "no-slug:", wantErr: true},
		{input: "repo:slug with spaces", wantErr: true},
		{input: "", wantErr: true},
	}

	for _, tt := range tests {
		got, err := ParseTarget(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseTarget(%q) expected error, got nil", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseTarget(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("ParseTarget(%q) = %+v, expected %+v", tt.input, got, tt.expected)
			}
		}
	}
}
