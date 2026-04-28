package tui_test

import (
	"strings"
	"testing"
)

func TestTrafficSeries_FixturesCarryContractShape(t *testing.T) {
	for _, name := range fixturesByPrefix(t, "T-") {
		t.Run(name, func(t *testing.T) {
			text := strings.ToLower(mustReadWireframeFixture(t, name))
			mustContain(t, text, "traffic")
		})
	}
}

func TestTrafficSeries_ResultAndScrollVocabulary(t *testing.T) {
	mustIncludeAny(t, "T-03_focused-inflight-row.txt", []string{"open", "chat"})
	mustIncludeAny(t, "T-05_full-retained-detail-open.txt", []string{"close", "traffic"})
	mustIncludeAny(t, "T-07_body-and-traffic-scroll-coexist.txt", []string{"traffic"})
}
