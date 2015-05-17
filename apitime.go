package main

import (
	"encoding/xml"
	"fmt"
	"time"
)

const ApiDateTimeFormat = "2006-01-02 15:04:05"

type APITime struct {
	time.Time
}

func (a *APITime) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	a.Time = time.Time{}

	for {
		t, _ := d.Token()
		if t == nil {
			return fmt.Errorf("couldn't find apitime end element, ran out of data!")
		}
		if t == start.End() {
			break
		}
		if timeBytes, ok := t.(xml.CharData); ok {
			var err error
			a.Time, err = time.Parse(ApiDateTimeFormat, string(timeBytes))
			if err != nil {
				return err
			}
		}
	}

	if a.Time.IsZero() {
		return fmt.Errorf("couldn't find a valid apitime.")
	}

	return nil
}

func (a *APITime) UnmarshalXMLAttr(attr xml.Attr) error {
	var err error
	a.Time, err = time.Parse(ApiDateTimeFormat, attr.Value)
	return err
}
