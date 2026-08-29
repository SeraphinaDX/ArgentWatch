package notify

import "testing"

func TestExpandTemplate(t *testing.T) {
	m := EmailMessage{Location: "Office", EventID: "abc"}
	got := ExpandTemplate("Alert {event_id} at {location}", m)
	if got != "Alert abc at Office" {
		t.Fatalf("got %q", got)
	}
}
