package client

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

func streamSSE(r io.Reader, onEvent func(event string, data string) error) error {
	br := bufio.NewReader(r)
	var (
		eventName string
		dataLines []string
	)

	flush := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		ev := eventName
		eventName = ""
		if onEvent == nil {
			return nil
		}
		return onEvent(ev, data)
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				_ = flush()
				return nil
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")

		// Blank line ends event.
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}

		// Comment.
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
	}
}
