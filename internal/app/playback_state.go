package app

import "github.com/dendec/pmv/internal/player"

type shuffleState struct {
	order    []trackRef
	idx      int
	albumIdx int
}

type trackRef struct {
	path     string
	album    string
	albumIdx int
	trackIdx int
}

type playbackState struct {
	pl      *player.Player
	lib     *player.Library
	shuffle shuffleState
}
