package screen

import "testing"

func TestDialog(t *testing.T) {
	rows := Rows([]string{
		"  quoted: Pick one",
		"  1. Yes",
		"",
		"  Pick one",
		"› 1.  Yes",
		"  2. No",
		"",
		"  enter confirm   ",
		"",
	})
	if !Dialog(rows, "Pick one", "enter confirm", "Yes", "No") {
		t.Fatal("expected the dialog to match")
	}
	if Dialog(rows, "Pick one", "enter confirm", "Maybe") {
		t.Fatal("an option that is not an entry must not match")
	}
	if Dialog(rows[:6], "Pick one", "enter confirm", "Yes") {
		t.Fatal("a dialog whose footer is not the last row must not match")
	}
	if Dialog(rows, "Other title", "enter confirm") {
		t.Fatal("a missing title must not match")
	}
}
