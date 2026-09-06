package app

import (
	"context"
	"errors"
	"time"
)

const calendarEventsURL = "https://www.googleapis.com/calendar/v3/calendars/primary/events"

type calendarTime struct {
	DateTime string `json:"dateTime"`
}
type calendarEvent struct {
	ID           string       `json:"id,omitempty"`
	Summary      string       `json:"summary"`
	Type         string       `json:"eventType"`
	Status       string       `json:"status,omitempty"`
	Transparency string       `json:"transparency"`
	Start        calendarTime `json:"start"`
	End          calendarTime `json:"end"`
	OutOfOffice  struct {
		Mode string `json:"autoDeclineMode"`
	} `json:"outOfOfficeProperties"`
	Extended struct {
		Private map[string]string `json:"private"`
	} `json:"extendedProperties"`
}

func googleStatus(err error, status int) bool {
	var failure *providerError
	return errors.As(err, &failure) && failure.status == status
}

func (a *Server) setCalendarStatus(ctx context.Context, c Connection, target string) error {
	if target == "available" && c.calendarEventID == "" {
		return nil
	}
	if target == "away" {
		end, err := time.Parse(time.RFC3339, c.AwayUntil)
		if err != nil || !end.After(time.Now()) {
			return &providerError{message: "Choose a future return time for Google Calendar."}
		}
		if c.calendarEventID == "" {
			c.calendarEventID = "ibb" + digest(randomToken())
			c.calendarStart = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		}
		// Reserve the ID before sending an insert. A lost response must never create
		// a second event on retry, including after a server restart.
		if _, err := a.store.db.ExecContext(ctx, `UPDATE connections SET calendar_event_id=$1,calendar_start=$2,calendar_end=$3 WHERE id=$4`, c.calendarEventID, c.calendarStart, c.AwayUntil, c.ID); err != nil {
			return err
		}
	}
	token, err := a.googleAccessToken(ctx, c)
	if err != nil {
		return err
	}
	endpoint := calendarEventsURL + "/" + c.calendarEventID
	var existing calendarEvent
	err = a.googleRequest(ctx, "GET", endpoint, token, nil, false, &existing)
	missing := googleStatus(err, 404) || googleStatus(err, 410) || (err == nil && existing.Status == "cancelled")
	if err != nil && !missing {
		return err
	}
	if target == "away" && (googleStatus(err, 410) || (err == nil && existing.Status == "cancelled")) {
		// A deleted event can retain its ID as a cancellation tombstone. Reuse
		// IDs only for uncertain inserts, never for a confirmed deletion.
		c.calendarEventID = "ibb" + digest(randomToken())
		c.calendarStart = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
		if _, err = a.store.db.ExecContext(ctx, `UPDATE connections SET calendar_event_id=$1,calendar_start=$2 WHERE id=$3`, c.calendarEventID, c.calendarStart, c.ID); err != nil {
			return err
		}
	}
	if !missing && (existing.ID != c.calendarEventID || existing.Type != "outOfOffice" || existing.Extended.Private["illBeBackConnection"] != c.ID) {
		return &providerError{message: "This Calendar event no longer matches the absence created here. It was left unchanged."}
	}
	if target == "available" {
		if !missing {
			err = a.googleRequest(ctx, "DELETE", endpoint, token, nil, false, nil)
			if err != nil && !googleStatus(err, 404) && !googleStatus(err, 410) {
				return err
			}
		}
		_, err = a.store.db.ExecContext(ctx, `UPDATE connections SET calendar_event_id='',calendar_start='',calendar_end='' WHERE id=$1`, c.ID)
		return err
	}
	event := calendarEvent{ID: c.calendarEventID, Summary: c.Message, Type: "outOfOffice", Transparency: "opaque", Start: calendarTime{c.calendarStart}, End: calendarTime{c.AwayUntil}}
	event.OutOfOffice.Mode = "declineNone"
	event.Extended.Private = map[string]string{"illBeBackConnection": c.ID}
	var confirmed calendarEvent
	if missing {
		err = a.googleRequest(ctx, "POST", calendarEventsURL, token, payload(event), false, &confirmed)
		// A concurrent/previous insert may have succeeded. Verify it on the next
		// retry before patching; never replace an event merely because its ID exists.
	} else {
		err = a.googleRequest(ctx, "PATCH", endpoint, token, payload(event), false, &confirmed)
	}
	if err != nil {
		return err
	}
	if confirmed.ID != event.ID || confirmed.Type != event.Type || confirmed.Summary != event.Summary || confirmed.Transparency != "opaque" || confirmed.OutOfOffice.Mode != "declineNone" || confirmed.Extended.Private["illBeBackConnection"] != c.ID || !sameInstant(confirmed.Start.DateTime, event.Start.DateTime) || !sameInstant(confirmed.End.DateTime, event.End.DateTime) {
		return &providerError{message: "Google Calendar hasn’t confirmed the requested absence. Please retry."}
	}
	return nil
}

func sameInstant(a, b string) bool {
	x, e1 := time.Parse(time.RFC3339, a)
	y, e2 := time.Parse(time.RFC3339, b)
	return e1 == nil && e2 == nil && x.Equal(y)
}
