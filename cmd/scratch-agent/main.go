package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"

	"pokearena"
	"pokearena/internal/agentloop"
	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/gwclient"
	"pokearena/internal/protocol"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/scratch-agent/main.go <battle_url>")
		os.Exit(1)
	}
	urlStr := os.Args[1]

	gatewayURL, battleID, slot, token, err := parseSlotURL(urlStr)
	if err != nil {
		log.Fatalf("Invalid URL: %v", err)
	}

	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		log.Fatalf("Failed to load dex: %v", err)
	}

	ctx := context.Background()
	gc, err := gwclient.Dial(ctx, gatewayURL, battleID, slot, token)
	if err != nil {
		log.Fatalf("Dial failed: %v", err)
	}
	defer gc.Close()

	fmt.Printf("Connected to battle %s as %s\n", battleID, slot)

	reader := bufio.NewReader(os.Stdin)

	for {
		select {
		case u, ok := <-gc.Updates():
			if !ok {
				if cerr := <-gc.Closed(); cerr != nil {
					log.Fatalf("Connection closed with error: %v", cerr)
				}
				fmt.Println("Connection closed cleanly.")
				return
			}

			switch u.Type {
			case protocol.FrameInfo:
				if u.Message != "" {
					fmt.Printf("[INFO] %s\n", u.Message)
				}
			case protocol.FrameError:
				fmt.Printf("[ERROR] %s\n", u.Message)
			case protocol.FrameState, protocol.FrameTurn:
				if u.View == nil {
					continue
				}
				v := *u.View
				acts := ai.LegalActions(v)
				if len(acts) == 0 {
					fmt.Printf("Turn %d: No legal actions available.\n", v.Turn)
					continue
				}

				// Display current state
				prompt := agentloop.RenderUserPrompt(dex, v, acts)
				fmt.Println("\n==================================================")
				fmt.Println(prompt)
				fmt.Println("==================================================")

				// Ask for user choice
				for {
					fmt.Printf("Enter choice index (0 to %d): ", len(acts)-1)
					text, err := reader.ReadString('\n')
					if err != nil {
						log.Fatalf("Failed to read input: %v", err)
					}
					text = strings.TrimSpace(text)
					idx, err := strconv.Atoi(text)
					if err != nil || idx < 0 || idx >= len(acts) {
						fmt.Println("Invalid index. Please choose a number in the range.")
						continue
					}

					chosenAction := acts[idx]
					// Send choice
					msg := toClientMsg(chosenAction)
					if err := gc.Send(msg); err != nil {
						log.Fatalf("Failed to send action: %v", err)
					}
					fmt.Printf("Sent action: %s\n", describeAction(dex, v, chosenAction))
					break
				}

			case protocol.FrameEnd:
				if u.Winner != nil {
					fmt.Printf("\nBattle ended! Winner: %d\n", *u.Winner)
				} else {
					fmt.Println("\nBattle ended with no winner.")
				}
				return
			}
		}
	}
}

func parseSlotURL(raw string) (gatewayURL, battleID, slot, token string, err error) {
	importURL, perr := url.Parse(raw)
	if perr != nil {
		err = perr
		return
	}
	q := importURL.Query()
	battleID = q.Get("battle")
	slot = q.Get("slot")
	token = q.Get("token")
	if battleID == "" || slot == "" || token == "" {
		err = fmt.Errorf("URL is missing one of battle/slot/token query params")
		return
	}
	ws := "ws"
	if importURL.Scheme == "https" {
		ws = "wss"
	}
	gatewayURL = ws + "://" + importURL.Host
	return
}

func toClientMsg(a engine.Action) protocol.WsClientMsg {
	kind := protocol.ActionKindMove
	if a.Kind == engine.ActionSwitch {
		kind = protocol.ActionKindSwitch
	}
	return protocol.WsClientMsg{Type: "action", Kind: kind, Index: a.Index}
}

func describeAction(dex *domain.Dex, v ai.View, a engine.Action) string {
	if a.Kind == engine.ActionSwitch {
		return "switch to " + v.Self.Team[a.Index].Name
	}
	if a.Index < 0 {
		return "Struggle"
	}
	return dex.Moves[v.Self.Team[v.Self.Active].Moves[a.Index].MoveID].Name
}
