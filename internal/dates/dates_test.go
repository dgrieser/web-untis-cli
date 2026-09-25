package dates

import (
	"testing"
	"time"
)

func TestParseRelativeTo(t *testing.T) {
	ref := time.Date(2026, 9, 24, 0, 0, 0, 0, time.Local) // Thursday
	cases := map[string]string{
		"":           "2026-09-24",
		"today":      "2026-09-24",
		"morgen":     "2026-09-25",
		"yesterday":  "2026-09-23",
		"monday":     "2026-09-28",
		"do":         "2026-09-24",
		"fr":         "2026-09-25",
		"+3d":        "2026-09-27",
		"-1w":        "2026-09-17",
		"2m":         "2026-11-24",
		"next-week":  "2026-09-28",
		"last-week":  "2026-09-14",
		"2026-09-21": "2026-09-21",
		"21.09.2026": "2026-09-21",
		"21.09.26":   "2026-09-21",
		"21.09.":     "2026-09-21",
		"1.10.":      "2026-10-01",
		"20261001":   "2026-10-01",
	}
	for in, want := range cases {
		got, err := ParseRelativeTo(in, ref)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if ISO(got) != want {
			t.Errorf("%q = %s, want %s", in, ISO(got), want)
		}
	}
	if _, err := ParseRelativeTo("nonsense", ref); err == nil {
		t.Error("expected error")
	}
}

func TestUntisFormats(t *testing.T) {
	d := FromIntTime(20260921, 745)
	if d.Format("2006-01-02 15:04") != "2026-09-21 07:45" {
		t.Fatal(d)
	}
	if Int(d) != 20260921 || HM(1305) != "13:05" {
		t.Fatal("int formats")
	}
	if Monday(time.Date(2026, 9, 27, 12, 0, 0, 0, time.Local)).Day() != 21 {
		t.Fatal("monday of sunday")
	}
	if Human(d) != "Mo 21.09.2026" {
		t.Fatal(Human(d))
	}
}
