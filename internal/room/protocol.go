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
//
// Server → client:
//
//	welcome  sent once after joining: your seat, reconnect token, board, timing
//	state    full room snapshot, sent after every change
//	moved    a dice roll to animate, sent just before the matching state
//	error    something was refused; "code" is machine-readable
const (
	msgStart = "start"
	msgRoll  = "roll"
	msgLeave = "leave"
	msgRules = "rules"
)

// Phases of a room.
const (
	PhaseLobby    = "lobby"
	PhasePlaying  = "playing"
	PhaseFinished = "finished"
)

type inMsg struct {
	T     string      `json:"t"`
	Rules *game.Rules `json:"rules,omitempty"`
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
	Playing   bool   `json:"playing"`
}

type stateMsg struct {
	T          string       `json:"t"`
	Room       string       `json:"room"`
	Phase      string       `json:"phase"`
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
	Name    string `json:"name"`
	Pos     int    `json:"pos"`
	Rolls   int    `json:"rolls"`
	Ladders int    `json:"ladders"`
	Snakes  int    `json:"snakes"`
	Won     bool   `json:"won"`
}

type movedMsg struct {
	T      string    `json:"t"`
	Move   game.Move `json:"move"`
	AnimMs int64     `json:"animMs"`
	Auto   bool      `json:"auto"` // rolled by the turn timer, not the player
}

type errorMsg struct {
	T    string `json:"t"`
	Code string `json:"code"`
}
