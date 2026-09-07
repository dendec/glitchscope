package ui

import (
	"strings"
	"testing"
	"time"
)

func TestMenuHintStartsAtFirstDrawAndExpires(t *testing.T) {
	now := time.Unix(100, 0)
	hint := menuHint{enabled: true}
	if hint.visible(now, true) || !hint.started.IsZero() {
		t.Fatal("menu consumed the hint timer")
	}
	if !hint.visible(now, false) {
		t.Fatal("hint missing on first frame")
	}
	if !hint.visible(now.Add(menuHintDuration-time.Millisecond), false) {
		t.Fatal("hint expired early")
	}
	if hint.visible(now.Add(menuHintDuration), false) {
		t.Fatal("hint did not expire")
	}
	if !hint.enabled {
		t.Fatal("timeout incorrectly acknowledged the hint")
	}
}

func TestOpeningMenuDismissesHint(t *testing.T) {
	o := &Overlay{menuHint: menuHint{enabled: true}}
	o.ToggleUI()
	if o.menuHint.enabled {
		t.Fatal("menu opening did not dismiss hint")
	}
	o.ToggleUI()
	if o.menuHint.visible(time.Now(), false) {
		t.Fatal("hint returned after closing menu")
	}
}

func TestHelpHasNoUnexpandedControls(t *testing.T) {
	for _, connected := range []bool{false, true} {
		o := &Overlay{controllerConnected: connected}
		check := func(lines []string) {
			for _, line := range o.helpLines(HelpTopic{Lines: lines}) {
				if strings.Contains(line, "{") {
					t.Fatalf("unexpanded placeholder: %s", line)
				}
			}
		}
		for i, topic := range helpTopics {
			if int(topic.ID) != i {
				t.Fatalf("topic ID does not match navigation index: %d", i)
			}
			check(topic.Lines)
			for _, entry := range topic.Children {
				check(entry.Lines)
				for _, sub := range entry.Children {
					check(sub.Lines)
				}
			}
		}
	}
}

func TestQuickStartPhysicalButtons(t *testing.T) {
	o := &Overlay{controllerConnected: true}
	lines := append([]string{}, helpTopic(HelpControls).Children[0].Lines...)
	lines = append(lines, helpTopic(HelpControls).Children[1].Lines...)
	text := strings.Join(o.helpLines(HelpTopic{Lines: lines}), "\n")
	for _, want := range []string{"START opens", "A opens", "B goes back", "X plays"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing physical button instruction %q in %s", want, text)
		}
	}
}

func TestShuffleDescriptionExplainsOfflineBehavior(t *testing.T) {
	if !strings.Contains(settingDescription(SettingShuffle, 3), "Offline") {
		t.Fatal("offline shuffle behavior missing")
	}
}
