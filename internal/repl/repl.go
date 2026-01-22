package repl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"testTask/internal/agent"
	"testTask/internal/logging"
)

type REPL struct {
	agent   agent.AgentController
	logger  *zap.Logger
	scanner *bufio.Scanner
}

func New(agent agent.AgentController, logger *logging.Logger) *REPL {
	return &REPL{
		agent:   agent,
		logger:  logger.Console(),
		scanner: bufio.NewScanner(os.Stdin),
	}
}

func (r *REPL) Run(ctx context.Context) error {
	r.printWelcome()

	for {
		fmt.Print("agent> ")
		if !r.scanner.Scan() {
			break
		}

		line := strings.TrimSpace(r.scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		cmd := parts[0]
		args := parts[1:]

		switch cmd {
		case "task":
			if len(args) == 0 {
				fmt.Println("Usage: task <description>")
				continue
			}
			task := strings.Join(args, " ")
			go func() {
				if err := r.agent.Start(ctx, task); err != nil {
					r.logger.Error("Task execution failed", zap.Error(err))
				}
			}()
			fmt.Printf("Started task: %s\n", task)

		case "status":
			status := r.agent.GetStatus()
			fmt.Println("Status:")
			for k, v := range status {
				fmt.Printf("  %s: %v\n", k, v)
			}

		case "pause":
			r.agent.Pause()
			fmt.Println("Agent paused")

		case "resume":
			r.agent.Resume()
			fmt.Println("Agent resumed")

		case "approve":
			if err := r.agent.Approve(); err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Println("Action approved")
			}

		case "deny":
			if err := r.agent.Deny(); err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Println("Action denied")
			}

		case "mode":
			if len(args) == 0 {
				fmt.Println("Usage: mode <live|dryrun>")
				continue
			}
			mode := args[0]
			if mode != "live" && mode != "dryrun" {
				fmt.Println("Mode must be 'live' or 'dryrun'")
				continue
			}
			dryRun := mode == "dryrun"
			r.agent.SetMode(dryRun)
			fmt.Printf("Mode set to: %s\n", mode)

		case "quit", "exit":
			r.agent.Stop()
			fmt.Println("Goodbye!")
			return nil

		case "help":
			r.printHelp()

		default:
			fmt.Printf("Unknown command: %s. Type 'help' for available commands.\n", cmd)
		}
	}

	return r.scanner.Err()
}

func (r *REPL) printWelcome() {
	fmt.Println("AI Agent REPL")
	fmt.Println("Type 'help' for available commands")
	fmt.Println()
}

func (r *REPL) printHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  task <description>     - Start a new task")
	fmt.Println("  status                 - Show agent status")
	fmt.Println("  pause                  - Pause agent execution")
	fmt.Println("  resume                 - Resume agent execution")
	fmt.Println("  approve                - Approve pending dangerous action")
	fmt.Println("  deny                   - Deny pending dangerous action")
	fmt.Println("  mode <live|dryrun>     - Set agent mode")
	fmt.Println("  help                   - Show this help")
	fmt.Println("  quit, exit             - Exit the REPL")
	fmt.Println()
}
