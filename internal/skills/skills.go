package skills

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Skill represents an installable prompt pack.
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // URL or "built-in"
	File        string `json:"file"`   // local path
}

// BuiltinSkills are the default skills that ship with MarsClaw.
var BuiltinSkills = []Skill{
	{ID: "coder", Name: "Coder", Description: "Fast, precise coding assistant — reads before editing, runs tests", Source: "built-in"},
	{ID: "devops", Name: "DevOps", Description: "Infrastructure, CI/CD, Docker, Kubernetes, cloud deployments", Source: "built-in"},
	{ID: "writer", Name: "Writer", Description: "Technical writing, documentation, blog posts, clear communication", Source: "built-in"},
	{ID: "analyst", Name: "Analyst", Description: "Data analysis, research, competitive intelligence, reports", Source: "built-in"},
	{ID: "compliance", Name: "Compliance Officer", Description: "GDPR, ISO 27001, EU AI Act — regulatory monitoring and gap analysis", Source: "built-in"},
}

// BuiltinPrompts maps skill IDs to their SOUL.md content.
var BuiltinPrompts = map[string]string{
	"coder": `You are MarsClaw, a fast and capable AI coding assistant.

Rules:
- Be concise and direct. Lead with the answer.
- Use tools to read files before editing them.
- Use edit_file for surgical changes, write_file for new files.
- Run shell commands to verify your work (tests, build).
- Never guess file contents — always read first.
- When you're done, say what you did in 1-2 sentences.`,

	"devops": `You are MarsClaw, a DevOps and infrastructure specialist.

Rules:
- Focus on reliability, security, and automation.
- Use infrastructure-as-code patterns (Terraform, Docker, k8s manifests).
- Always check current state before making changes (kubectl get, docker ps, etc).
- Prefer declarative over imperative approaches.
- Validate configurations before applying (--dry-run, terraform plan).
- Document any manual steps that can't be automated yet.`,

	"writer": `You are MarsClaw, a technical writing assistant.

Rules:
- Write clearly and concisely. No filler words.
- Use active voice. Lead with the key point.
- Structure content with headers, bullets, and short paragraphs.
- Match the tone and style of existing documentation.
- Include code examples when explaining technical concepts.
- Proofread for clarity, not just grammar.`,

	"analyst": `You are MarsClaw, a research and analysis assistant.

Rules:
- Start with the conclusion, then supporting evidence.
- Use data and specific numbers over vague claims.
- Compare alternatives with clear pros/cons.
- Cite sources when making factual claims.
- Flag assumptions and uncertainties explicitly.
- Present findings in structured tables when useful.`,

	"compliance": `You are MarsClaw, a compliance and regulatory specialist for European regulations.

Rules:
- Reference specific articles and clauses (e.g. GDPR Art. 30, ISO 27001 A.8).
- Prioritize findings by risk level (critical, high, medium, low).
- Generate audit-ready documentation with proper formatting.
- Track regulatory changes and flag upcoming deadlines.
- Provide actionable remediation steps, not just findings.
- Maintain records of processing activities and DPIAs.`,
}

// Dir returns the skills directory path.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".marsclaw", "skills")
}

// ActiveFile returns the path to the active skill symlink/marker.
func ActiveFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".marsclaw", "active_skill")
}

// Install downloads a skill from a URL and saves it locally.
func Install(url, name string) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}

	// Sanitize name for filename.
	safe := strings.ReplaceAll(strings.ToLower(name), " ", "-")
	safe = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, safe)

	path := filepath.Join(dir, safe+".md")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return err
	}

	return nil
}

// InstallBuiltin saves a built-in skill prompt to disk.
func InstallBuiltin(id string) error {
	prompt, ok := BuiltinPrompts[id]
	if !ok {
		return fmt.Errorf("unknown built-in skill: %s", id)
	}

	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	path := filepath.Join(dir, id+".md")
	return os.WriteFile(path, []byte(prompt), 0o644)
}

// SetActive marks a skill as the active SOUL.md.
func SetActive(id string) error {
	return os.WriteFile(ActiveFile(), []byte(id), 0o644)
}

// GetActive returns the currently active skill ID.
func GetActive() string {
	data, err := os.ReadFile(ActiveFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// GetActivePrompt returns the prompt content for the active skill.
func GetActivePrompt() string {
	id := GetActive()
	if id == "" {
		return ""
	}

	// Check built-in first.
	if prompt, ok := BuiltinPrompts[id]; ok {
		return prompt
	}

	// Check installed files.
	path := filepath.Join(Dir(), id+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// ListInstalled returns all installed skill files.
func ListInstalled() []string {
	dir := Dir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return names
}

// RunList shows all available skills.
func RunList() error {
	active := GetActive()
	installed := ListInstalled()
	installedSet := make(map[string]bool)
	for _, s := range installed {
		installedSet[s] = true
	}

	fmt.Print("\n  \033[1mMarsClaw Skills\033[0m\n\n")

	fmt.Println("  \033[36mBuilt-in:\033[0m")
	for _, s := range BuiltinSkills {
		marker := "  "
		if s.ID == active {
			marker = "\033[32m●\033[0m "
		}
		fmt.Printf("    %s%-18s %s\n", marker, s.ID, s.Description)
	}

	if len(installed) > 0 {
		fmt.Println()
		fmt.Println("  \033[36mInstalled:\033[0m")
		for _, name := range installed {
			if _, isBuiltin := BuiltinPrompts[name]; isBuiltin {
				continue
			}
			marker := "  "
			if name == active {
				marker = "\033[32m●\033[0m "
			}
			fmt.Printf("    %s%s\n", marker, name)
		}
	}

	if active != "" {
		fmt.Printf("\n  Active: \033[32m%s\033[0m\n", active)
	} else {
		fmt.Println("\n  No active skill (using SOUL.md or default)")
	}
	fmt.Println()

	return nil
}

// RunInstall installs a skill by name or URL.
func RunInstall(source string) error {
	// Check if it's a built-in skill ID.
	if _, ok := BuiltinPrompts[source]; ok {
		if err := InstallBuiltin(source); err != nil {
			return err
		}
		fmt.Printf("  \033[32m✓ Installed built-in skill: %s\033[0m\n", source)
		if confirmActivate() {
			SetActive(source)
			fmt.Printf("  \033[32m✓ Active skill set to: %s\033[0m\n\n", source)
		}
		return nil
	}

	// Check if it's a URL.
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("  Skill name: ")
		name, _ := reader.ReadString('\n')
		name = strings.TrimSpace(name)
		if name == "" {
			name = "custom-skill"
		}

		fmt.Printf("  Downloading from %s...\n", source)
		if err := Install(source, name); err != nil {
			return err
		}

		safe := strings.ReplaceAll(strings.ToLower(name), " ", "-")
		fmt.Printf("  \033[32m✓ Installed: %s\033[0m\n", safe)
		if confirmActivate() {
			SetActive(safe)
			fmt.Printf("  \033[32m✓ Active skill set to: %s\033[0m\n\n", safe)
		}
		return nil
	}

	return fmt.Errorf("unknown skill %q — use a built-in ID or URL", source)
}

// RunUse sets the active skill.
func RunUse(id string) error {
	// Verify it exists.
	if _, ok := BuiltinPrompts[id]; !ok {
		path := filepath.Join(Dir(), id+".md")
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("skill %q not found — run: marsclaw skills list", id)
		}
	}

	SetActive(id)
	fmt.Printf("  \033[32m✓ Active skill: %s\033[0m\n\n", id)
	return nil
}

func confirmActivate() bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("  Set as active skill? [Y/n] ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}
