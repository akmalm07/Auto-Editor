package loading

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"golang.org/x/term"
)

type Settings struct {
	Enabled    bool
	Style      string
	IntervalMS int
	TTY        *os.File
}

type Session struct {
	out      io.Writer
	enabled  bool
	settings Settings
	mu       sync.Mutex
	spin     *spinner.Spinner
}

var asciiFrames = map[string][]string{
	"ascii": {
		"[    ]",
		"[=   ]",
		"[==  ]",
		"[=== ]",
		"[ ===]",
		"[  ==]",
		"[   =]",
	},
	"dots": {
		"[    ]",
		"[.   ]",
		"[..  ]",
		"[... ]",
		"[ .. ]",
		"[  . ]",
	},
	"bar": {
		"[    ]",
		"[-   ]",
		"[--  ]",
		"[--- ]",
		"[ ---]",
		"[  --]",
		"[   -]",
	},
}

func New(out io.Writer, settings Settings) *Session {
	enabled := settings.Enabled && settings.TTY != nil && term.IsTerminal(int(settings.TTY.Fd()))

	return &Session{
		out:      out,
		enabled:  enabled,
		settings: settings,
	}
}

func (s *Session) Start(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		fmt.Fprintln(s.out, message)
		return
	}

	frames := framesFor(s.settings.Style)
	interval := time.Duration(s.settings.IntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = 80 * time.Millisecond
	}
	spin := spinner.New(frames, interval, spinner.WithWriter(s.out), spinner.WithWriterFile(s.settings.TTY))
	spin.Prefix = message + " "
	spin.Start()
	s.spin = spin
}

func (s *Session) Update(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled || s.spin == nil {
		fmt.Fprintln(s.out, message)
		return
	}

	s.spin.Prefix = message + " "
}

func (s *Session) Stop(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.enabled && s.spin != nil {
		s.spin.Stop()
		s.spin = nil
	}

	if message != "" {
		fmt.Fprintln(s.out, message)
	}
}

func framesFor(style string) []string {
	if frames, ok := asciiFrames[strings.ToLower(strings.TrimSpace(style))]; ok {
		return frames
	}
	return asciiFrames["ascii"]
}
