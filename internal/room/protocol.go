package room

import "github.com/erwinwahyura/snake-ladder/internal/game"

// Messages are JSON objects with a "t" (type) field.
//
// Client → server:
//
//	{"t":"start"}   host starts a game, or a rematch after one ends
//	{"t":"roll"}    roll the dice on your turn
//	{"t":"leave"}   give up your seat
//	{"t":"rules","rules":{"overshoot":"stay","sixAgain":true}}   host, before start
//	{"t":"react","emoji":"😂"}   seated players; one of Reactions, rate-limited
//	{"t":"kick","seat":2}        host, outside a game, only a disconnected seat
//	{"t":"config","size":3,"public":true}       host, outside a game
//	{"t":"bot","action":"add"|"remove"|"fill","seat":1}   host, outside a game
//
// Server → client:
//
//	welcome  sent once after joining: your seat, reconnect token, board, timing
//	state    full room snapshot, sent after every change
//	moved    a dice roll to animate, sent just before the matching state
//	reaction someone at the table sent an emoji
//	error    something was refused; "code" is machine-readable
const (
	msgStart  = "start"
	msgRoll   = "roll"
	msgLeave  = "leave"
	msgRules  = "rules"
	msgReact  = "react"
	msgKick   = "kick"
	msgConfig = "config"
	msgBot    = "bot"
)

// Reactions are the only emoji the server relays. A fixed set keeps the
// table friendly and the messages tiny.
var Reactions = []string{"😂", "😱", "🔥", "👏", "😭", "🐍"}

func validReaction(e string) bool {
	for _, r := range Reactions {
		if r == e {
			return true
		}
	}
	return false
}

// Phases of a room.
const (
	PhaseLobby    = "lobby"
	PhasePlaying  = "playing"
	PhaseFinished = "finished"
)

type inMsg struct {
	T      string      `json:"t"`
	Rules  *game.Rules `json:"rules,omitempty"`
	Emoji  string      `json:"emoji,omitempty"`
	Seat   *int        `json:"seat,omitempty"`
	Size   *int        `json:"size,omitempty"`
	Public *bool       `json:"public,omitempty"`
	Action string      `json:"action,omitempty"`
}

type boardView struct {
	Ladders []game.Jump `json:"ladders"`
	Snakes  []game.Jump `json:"snakes"`
}

type timingView struct {
	DiceMs int64 `json:"diceMs"`
	StepMs int64 `json:"stepMs"`
	JumpMs int64 `json:"jumpMs"`
}

type welcomeMsg struct {
	T      string     `json:"t"`
	Room   string     `json:"room"`
	Seat   int        `json:"seat"` // -1 for spectators
	Token  string     `json:"token,omitempty"`
	Board  boardView  `json:"board"`
	Timing timingView `json:"timing"`
}

type playerView struct {
	Seat      int    `json:"seat"`
	Name      string `json:"name"`
	Pos       int    `json:"pos"`
	Connected bool   `json:"connected"`
	Host      bool   `json:"host"`
	Bot       bool   `json:"bot"`
	Playing   bool   `json:"playing"`
}

type stateMsg struct {
	T          string       `json:"t"`
	Room       string       `json:"room"`
	Phase      string       `json:"phase"`
	Size       int          `json:"size"`   // seats in use: 2, 3 or 4
	Public     bool         `json:"public"` // listed under "Meja terbuka"
	Rules      game.Rules   `json:"rules"`
	Players    []playerView `json:"players"`
	Turn       int          `json:"turn"`     // seat, -1 if none
	Winner     int          `json:"winner"`   // seat, -1 if none
	Deadline   int64        `json:"deadline"` // unix ms when the turn auto-rolls, 0 if none
	Now        int64        `json:"now"`      // server unix ms, to correct client clocks
	Spectators int          `json:"spectators"`
	Results    []resultView `json:"results,omitempty"` // when finished
	Ranked     bool         `json:"ranked"`            // counts for the leaderboard
}

type resultView struct {
	Seat    int    `json:"seat"`
	Place   int    `json:"place"`  // 1 = winner; equal squares share a place
	Points  int    `json:"points"` // leaderboard points earned (ranked games only)
	Name    string `json:"name"`
	Pos     int    `json:"pos"`
	Rolls   int    `json:"rolls"`
	Ladders int    `json:"ladders"`
	Snakes  int    `json:"snakes"`
	Won     bool   `json:"won"`
	Bot     bool   `json:"bot"`
}

type movedMsg struct {
	T      string    `json:"t"`
	Move   game.Move `json:"move"`
	AnimMs int64     `json:"animMs"`
	Auto   bool      `json:"auto"` // rolled by the turn timer, not the player
}

type reactionMsg struct {
	T     string `json:"t"`
	Seat  int    `json:"seat"`
	Emoji string `json:"emoji"`
}

type errorMsg struct {
	T    string `json:"t"`
	Code string `json:"code"`
}
