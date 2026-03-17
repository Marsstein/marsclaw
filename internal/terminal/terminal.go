package terminal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/marsstein/liteclaw/internal/agent"
	t "github.com/marsstein/liteclaw/internal/types"
)

const (
	colorReset  = "\033[0m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
)

// Session runs an interactive terminal conversation.
type Session struct {
	agent   *agent.Agent
	cost    t.CostRecorder
	model   string
	history []t.Message
	soul    string
}

// NewSession creates an interactive session.
func NewSession(a *agent.Agent, cost t.CostRecorder, model, soul string) *Session {
	return &Session{
		agent:   a,
		cost:    cost,
		model:   model,
		history: make([]t.Message, 0, 64),
		soul:    soul,
	}
}

// Run starts the interactive loop.
func (s *Session) Run(ctx context.Context) error {
	s.printBanner()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	runCtx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	for {
		fmt.Printf("\n%s%s> %s", colorBold, colorCyan, colorReset)

		if !scanner.Scan() {
			fmt.Println()
			return nil
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		switch input {
		case "/quit", "/exit", "/q":
			fmt.Printf("%sBye!%s\n", colorDim, colorReset)
			return nil
		case "/clear":
			s.history = s.history[:0]
			fmt.Printf("%sConversation cleared.%s\n", colorDim, colorReset)
			continue
		case "/history":
			s.printHistory()
			continue
		case "/help":
			s.printHelp()
			continue
		}

		s.history = append(s.history, t.Message{
			Role:      t.RoleUser,
			Content:   input,
			Timestamp: time.Now(),
		})

		parts := t.ContextParts{
			SoulPrompt: s.soul,
			History:    s.history,
		}

		fmt.Println()
		result := s.agent.Run(runCtx, parts)

		// Use the agent's full history (includes tool calls/results).
		if len(result.History) > 0 {
			s.history = result.History
		} else {
			s.history = append(s.history, t.Message{
				Role:      t.RoleAssistant,
				Content:   result.Response,
				Timestamp: time.Now(),
			})
		}

		// If streaming was off, print the response.
		if result.Response != "" && result.StopReason == t.StopError {
			fmt.Printf("%s%sError: %v%s\n", colorRed, colorBold, result.Error, colorReset)
		}

		fmt.Println()

		// Cost line.
		if s.cost != nil {
			costLine := s.cost.FormatCostLine(s.model, result.TotalInput, result.TotalOutput)
			fmt.Printf("%s%s%s\n", colorDim, costLine, colorReset)
		}

		if result.StopReason == t.StopError {
			fmt.Printf("%sAgent stopped: %s%s\n", colorYellow, result.StopReason, colorReset)
		}
	}
}

func (s *Session) printBanner() {
	fmt.Printf(`
%s%s  _     _ _        ____ _
 | |   (_) |_ ___ / ___| | __ ___      __
 | |   | | __/ _ \ |   | |/ _`+"`"+` \ \ /\ / /
 | |___| | ||  __/ |___| | (_| |\ V  V /
 |_____|_|\__\___|\____|_|\__,_| \_/\_/  %s

%s  Lightweight, secure, multi-agent AI runtime%s
%s  Model: %s │ Type /help for commands%s

`, colorBold, colorCyan, colorReset, colorDim, colorReset, colorDim, s.model, colorReset)
}

func (s *Session) printHelp() {
	fmt.Printf(`
%sCommands:%s
  /help      Show this help
  /clear     Clear conversation history
  /history   Show message history
  /quit      Exit LiteClaw

%sAvailable tools:%s read_file, write_file, edit_file, shell, list_files, search
`, colorBold, colorReset, colorBold, colorReset)
}

func (s *Session) printHistory() {
	if len(s.history) == 0 {
		fmt.Printf("%sNo messages yet.%s\n", colorDim, colorReset)
		return
	}

	for _, msg := range s.history {
		role := string(msg.Role)
		color := colorDim
		switch msg.Role {
		case t.RoleUser:
			color = colorCyan
		case t.RoleAssistant:
			color = colorGreen
		case t.RoleTool:
			color = colorYellow
		}
		content := msg.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		fmt.Printf("%s[%s]%s %s\n", color, role, colorReset, content)
	}
}
