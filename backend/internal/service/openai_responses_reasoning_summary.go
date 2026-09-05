package service

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ensureResponsesReasoningSummary makes Responses input reasoning items
// protocol-compliant immediately before they are sent upstream. The helper is
// deliberately additive and idempotent: existing summaries, including empty
// arrays, are preserved; only missing or null summaries become [].
func ensureResponsesReasoningSummary(body []byte) ([]byte, bool, error) {
	if len(bytes.TrimSpace(body)) == 0 || !gjson.ValidBytes(body) {
		return body, false, nil
	}

	input := gjson.GetBytes(body, "input")
	if !input.Exists() || input.Type == gjson.Null || input.Type == gjson.String {
		return body, false, nil
	}

	if input.IsObject() {
		updated, changed, err := ensureResponsesReasoningSummaryItem([]byte(input.Raw))
		if err != nil {
			return body, false, fmt.Errorf("normalize input object: %w", err)
		}
		if !changed {
			return body, false, nil
		}
		out, err := sjson.SetRawBytes(body, "input", updated)
		if err != nil {
			return body, false, fmt.Errorf("replace input object: %w", err)
		}
		return out, true, nil
	}

	if !input.IsArray() {
		return body, false, nil
	}

	items := make([][]byte, 0)
	changed := false
	var itemErr error
	input.ForEach(func(_, item gjson.Result) bool {
		itemBody := []byte(item.Raw)
		updated, itemChanged, err := ensureResponsesReasoningSummaryItem(itemBody)
		if err != nil {
			itemErr = err
			return false
		}
		if itemChanged {
			changed = true
			itemBody = updated
		}
		items = append(items, itemBody)
		return true
	})
	if itemErr != nil {
		return body, false, fmt.Errorf("normalize input item: %w", itemErr)
	}

	if !changed {
		return body, false, nil
	}
	rebuilt := make([]byte, 0, len(input.Raw)+len(items)*2)
	rebuilt = append(rebuilt, '[')
	for index, item := range items {
		if index > 0 {
			rebuilt = append(rebuilt, ',')
		}
		rebuilt = append(rebuilt, item...)
	}
	rebuilt = append(rebuilt, ']')
	out, err := sjson.SetRawBytes(body, "input", rebuilt)
	if err != nil {
		return body, false, fmt.Errorf("replace input array: %w", err)
	}
	return out, true, nil
}

func ensureResponsesReasoningSummaryItem(item []byte) ([]byte, bool, error) {
	parsed := gjson.ParseBytes(item)
	if !parsed.IsObject() || strings.TrimSpace(parsed.Get("type").String()) != "reasoning" {
		return item, false, nil
	}
	summary := parsed.Get("summary")
	if summary.Exists() && summary.Type != gjson.Null {
		return item, false, nil
	}
	updated, err := sjson.SetRawBytes(item, "summary", []byte("[]"))
	if err != nil {
		return item, false, err
	}
	return updated, true, nil
}
