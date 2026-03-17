package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marsstein/marsclaw/internal/agent"
	"github.com/marsstein/marsclaw/internal/store"
	t "github.com/marsstein/marsclaw/internal/types"
)

// Task is a recurring scheduled job.
type Task struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"` // cron-like: "0 9 * * 1-5" or interval: "every 30m"
	Prompt   string `json:"prompt"`   // what the agent should do
	Channel  string `json:"channel"`  // where to send results: "telegram:chatid", "log", etc.
	Enabled  bool   `json:"enabled"`
}

// Sender delivers a task result to a channel.
type Sender func(ctx context.Context, channel, message string) error

// Scheduler runs recurring tasks.
type Scheduler struct {
	tasks   []Task
	agent   *agent.Agent
	soul    string
	store   store.Store
	sender  Sender
	logger  *slog.Logger

	mu      sync.Mutex
	cancel  context.CancelFunc
}

// New creates a scheduler.
func New(tasks []Task, a *agent.Agent, soul string, sender Sender, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		tasks:  tasks,
		agent:  a,
		soul:   soul,
		sender: sender,
		logger: logger,
	}
}

// Run starts the scheduler loop. Blocks until context is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	s.logger.Info("scheduler started", "tasks", len(s.tasks))

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Check immediately on startup.
	s.checkTasks(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.checkTasks(ctx)
		}
	}
}

func (s *Scheduler) checkTasks(ctx context.Context) {
	now := time.Now()
	for _, task := range s.tasks {
		if !task.Enabled {
			continue
		}
		if !shouldRun(task.Schedule, now) {
			continue
		}
		go s.runTask(ctx, task)
	}
}

func (s *Scheduler) runTask(ctx context.Context, task Task) {
	s.logger.Info("running scheduled task", "task", task.Name)

	parts := t.ContextParts{
		SoulPrompt: s.soul,
		History: []t.Message{{
			Role:      t.RoleUser,
			Content:   task.Prompt,
			Timestamp: time.Now(),
		}},
	}

	result := s.agent.Run(ctx, parts)

	response := result.Response
	if response == "" {
		response = fmt.Sprintf("Task %q completed with no output.", task.Name)
	}

	if s.sender != nil {
		msg := fmt.Sprintf("**[Scheduled: %s]**\n\n%s", task.Name, response)
		if err := s.sender(ctx, task.Channel, msg); err != nil {
			s.logger.Error("failed to send task result", "task", task.Name, "error", err)
		}
	}

	s.logger.Info("scheduled task complete", "task", task.Name, "stop", result.StopReason)
}

// shouldRun checks if a task should execute at the given time.
// Supports:
//   - "every 5m", "every 1h", "every 30s"
//   - Simple cron: "M H * * *" (minute hour * * day-of-week)
func shouldRun(schedule string, now time.Time) bool {
	// Interval format: "every 30m"
	if strings.HasPrefix(schedule, "every ") {
		dur, err := time.ParseDuration(strings.TrimPrefix(schedule, "every "))
		if err != nil {
			return false
		}
		// Run if current time aligns to the interval (within 30s window).
		secs := int64(dur.Seconds())
		if secs <= 0 {
			return false
		}
		return now.Unix()%secs < 30
	}

	// Simple cron: "minute hour * * day-of-week"
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return false
	}

	// Only trigger on the exact minute (within 30s window).
	if now.Second() >= 30 {
		return false
	}

	return matchCronField(parts[0], now.Minute()) &&
		matchCronField(parts[1], now.Hour()) &&
		matchCronField(parts[2], now.Day()) &&
		matchCronField(parts[3], int(now.Month())) &&
		matchCronField(parts[4], int(now.Weekday()))
}

func matchCronField(field string, value int) bool {
	if field == "*" {
		return true
	}

	// Handle ranges: "1-5"
	if strings.Contains(field, "-") {
		parts := strings.SplitN(field, "-", 2)
		lo, err1 := strconv.Atoi(parts[0])
		hi, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return false
		}
		return value >= lo && value <= hi
	}

	// Handle lists: "1,3,5"
	for _, part := range strings.Split(field, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && n == value {
			return true
		}
	}

	return false
}
