package usage

import "testing"

func TestParseNFTCounters(t *testing.T) {
	data := []byte(`{"nftables":[{"metainfo":{"version":"1.0"}},{"rule":{"comment":"wraplink:route-usage:192.0.2.10:upload:lease-a","expr":[{"match":{"left":"a","right":"b"}},{"counter":{"packets":4,"bytes":8192}}]}},{"rule":{"comment":"wraplink:route-usage:192.0.2.10:download:lease-a","expr":[{"counter":{"packets":2,"bytes":4096}}]}},{"rule":{"comment":"unrelated","expr":[{"counter":{"bytes":99999}}]}}]}`)
	counters, err := ParseNFTCounters(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(counters) != 2 || counters[0].Bytes != 8192 || counters[1].Bytes != 4096 {
		t.Fatalf("wrong counters: %+v", counters)
	}
}

func TestParseNFTCountersRejectsAmbiguousRules(t *testing.T) {
	cases := []string{
		`{}`,
		`{"nftables":null}`,
		`{"nftables":[{"rule":{"comment":"wraplink:route-usage:192.0.2.1:upload:a","expr":[]}}]}`,
		`{"nftables":[{"rule":{"comment":"wraplink:route-usage:192.0.2.1:upload:a","expr":[{"counter":{"bytes":1}},{"counter":{"bytes":2}}]}}]}`,
		`{"nftables":[{"rule":{"comment":"wraplink:route-usage:invalid:upload:a","expr":[{"counter":{"bytes":1}}]}}]}`,
	}
	for _, sample := range cases {
		if _, err := ParseNFTCounters([]byte(sample)); err == nil {
			t.Fatalf("accepted invalid sample: %s", sample)
		}
	}
}
