package main

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3})?(Z|[+-](0\d|1\d|2[0-3]):[0-5]\d)$`)

func timestamp(value string) (time.Time, error) {
	if !timestampPattern.MatchString(value) {
		return time.Time{}, errors.New("invalid RFC3339 millisecond timestamp")
	}
	return time.Parse(time.RFC3339Nano, value)
}

type object map[string]json.RawMessage

func decodeObject(data []byte) (object, error) {
	var value object
	if err := json.Unmarshal(data, &value); err != nil || value == nil {
		return nil, errors.New("expected JSON object")
	}
	return value, nil
}

func array(value json.RawMessage) ([]json.RawMessage, error) {
	var result []json.RawMessage
	if len(value) == 0 || json.Unmarshal(value, &result) != nil || result == nil {
		return nil, errors.New("expected non-null array")
	}
	return result, nil
}

func stringField(value object, field string) (string, error) {
	var result string
	raw := value[field]
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &result) != nil {
		return "", errors.New("missing or invalid string field: " + field)
	}
	return result, nil
}

func timeField(value object, field string) (time.Time, error) {
	text, err := stringField(value, field)
	if err != nil {
		return time.Time{}, err
	}
	return timestamp(text)
}

func integerField(value object, field string) (int64, error) {
	var result int64
	raw := value[field]
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &result) != nil {
		return 0, errors.New("missing or invalid integer field: " + field)
	}
	return result, nil
}

func numberField(value object, field string) error {
	var result float64
	raw := value[field]
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &result) != nil {
		return errors.New("missing or invalid numeric field: " + field)
	}
	return nil
}

func validateResponse(body []byte, command, filename, since string) (string, error) {
	root, err := decodeObject(body)
	if err != nil {
		return "", err
	}
	var upper, lower time.Time
	var books []json.RawMessage
	watermark := ""
	if command == "sync" {
		watermark, err = stringField(root, "watermark")
		if err != nil {
			return "", err
		}
		upper, err = timestamp(watermark)
		if err != nil {
			return "", err
		}
		baseline, err := timeField(root, "baseline_at")
		if err != nil || baseline.After(upper) {
			return "", errors.New("invalid baseline_at")
		}
		if since != "" {
			lower, err = timestamp(since)
			if err != nil || lower.After(upper) {
				return "", errors.New("watermark precedes requested since")
			}
		}
		books, err = array(root["books"])
		if err != nil {
			return "", err
		}
	} else {
		books = []json.RawMessage{root["book"]}
	}
	parents := map[string]time.Time{}
	counts := map[string]int64{}
	for _, raw := range books {
		name, aggregate, err := validateBook(raw, upper)
		if err != nil {
			return "", err
		}
		if _, duplicate := parents[name]; duplicate {
			return "", errors.New("duplicate book")
		}
		if command == "book" && name != filename {
			return "", errors.New("book filename does not match request")
		}
		if !lower.IsZero() && aggregate.Before(lower) {
			return "", errors.New("book precedes requested sync window")
		}
		parents[name] = aggregate
		book, err := decodeObject(raw)
		if err != nil {
			return "", err
		}
		count, err := integerField(book, "annot_count")
		if err != nil || count < 0 {
			return "", errors.New("invalid book annot_count")
		}
		counts[name] = count
	}
	annotations, err := array(root["annotations"])
	if err != nil {
		return "", err
	}
	keys := map[int64]bool{}
	returned := map[string]int64{}
	for _, raw := range annotations {
		annotation, err := decodeObject(raw)
		if err != nil {
			return "", err
		}
		name, err := stringField(annotation, "filename")
		if err != nil {
			return "", err
		}
		parent, ok := parents[name]
		if !ok {
			return "", errors.New("annotation has no returned parent book")
		}
		id, err := integerField(annotation, "id")
		if err != nil || id <= 0 {
			return "", errors.New("invalid annotation id")
		}
		if keys[id] {
			return "", errors.New("duplicate annotation")
		}
		keys[id] = true
		returned[name]++
		for _, field := range []string{"ann_id", "created_at"} {
			if _, err := integerField(annotation, field); err != nil {
				return "", err
			}
		}
		for _, field := range []string{"text", "note"} {
			if _, err := stringField(annotation, field); err != nil {
				return "", err
			}
		}
		updated, err := timeField(annotation, "updated_at")
		if err != nil || updated.After(parent) || (!lower.IsZero() && updated.Before(lower)) {
			return "", errors.New("invalid annotation updated_at")
		}
	}
	for name, count := range counts {
		if returned[name] > count || (command == "book" && returned[name] != count) {
			return "", errors.New("returned annotations do not match current annot_count")
		}
	}
	return watermark, nil
}

func validateBook(raw json.RawMessage, upper time.Time) (string, time.Time, error) {
	book, err := decodeObject(raw)
	if err != nil {
		return "", time.Time{}, err
	}
	name, err := stringField(book, "filename")
	if err != nil || name == "" {
		return "", time.Time{}, errors.New("missing book filename")
	}
	if _, err := stringField(book, "title"); err != nil {
		return "", time.Time{}, err
	}
	authors, err := array(book["authors"])
	if err != nil {
		return "", time.Time{}, err
	}
	for _, raw := range authors {
		var author string
		if string(raw) == "null" || json.Unmarshal(raw, &author) != nil {
			return "", time.Time{}, errors.New("invalid author")
		}
	}
	updated, err := timeField(book, "updated_at")
	if err != nil {
		return "", time.Time{}, err
	}
	aggregate, err := timeField(book, "sync_updated_at")
	if err != nil || aggregate.Before(updated) || (!upper.IsZero() && aggregate.After(upper)) {
		return "", time.Time{}, errors.New("invalid book sync_updated_at")
	}
	links, err := decodeObject(book["links"])
	if err != nil {
		return "", time.Time{}, err
	}
	for _, field := range []string{"ui", "cover"} {
		link, err := stringField(links, field)
		if err != nil || link == "" {
			return "", time.Time{}, errors.New("missing book link")
		}
	}
	for _, field := range []string{"position", "reading_stats"} {
		raw := book[field]
		if len(raw) == 0 {
			return "", time.Time{}, errors.New("missing book field: " + field)
		}
		if string(raw) == "null" {
			continue
		}
		child, err := decodeObject(raw)
		if err != nil {
			return "", time.Time{}, err
		}
		if field == "position" || string(child["updated_at"]) != "null" {
			childTime, err := timeField(child, "updated_at")
			if err != nil || childTime.After(aggregate) {
				return "", time.Time{}, errors.New("invalid child updated_at")
			}
		}
		if field == "position" {
			for _, location := range []string{"chapter", "split_index", "char_offset", "page"} {
				if string(child[location]) == "null" {
					continue
				}
				if _, err := integerField(child, location); err != nil {
					return "", time.Time{}, err
				}
			}
			if err := numberField(child, "pct"); err != nil {
				return "", time.Time{}, err
			}
			if _, err := integerField(child, "last_read"); err != nil {
				return "", time.Time{}, err
			}
		}
		if field == "reading_stats" {
			nullTotals := string(child["updated_at"]) == "null"
			for _, field := range []string{"used_time_ms", "read_words"} {
				if nullTotals {
					if string(child[field]) != "null" {
						return "", time.Time{}, errors.New("inconsistent unavailable reading totals")
					}
				} else if _, err := integerField(child, field); err != nil {
					return "", time.Time{}, err
				}
			}
			days, err := array(child["days"])
			if err != nil {
				return "", time.Time{}, err
			}
			seen := map[int64]bool{}
			for _, raw := range days {
				day, err := decodeObject(raw)
				if err != nil {
					return "", time.Time{}, err
				}
				id, err := integerField(day, "day")
				if err != nil || seen[id] {
					return "", time.Time{}, errors.New("invalid or duplicate reading day")
				}
				seen[id] = true
				for _, field := range []string{"read_time_ms", "words_read"} {
					if _, err := integerField(day, field); err != nil {
						return "", time.Time{}, err
					}
				}
				if err := numberField(day, "pct"); err != nil {
					return "", time.Time{}, err
				}
				updated, err := timeField(day, "updated_at")
				if err != nil || updated.After(aggregate) {
					return "", time.Time{}, errors.New("invalid reading day updated_at")
				}
			}
		}
	}
	return name, aggregate, nil
}
