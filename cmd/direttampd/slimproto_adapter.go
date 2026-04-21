package main

import (
	"github.com/famish99/direttampd/internal/player"
	"github.com/famish99/direttampd/internal/slimproto"
)

// slimprotoPlayerAdapter wraps *player.Player to satisfy slimproto.Controller
// without leaking the player package into internal/slimproto.
type slimprotoPlayerAdapter struct {
	p *player.Player
}

func (a slimprotoPlayerAdapter) ClearPlaylist()            { a.p.GetPlaylist().Clear() }
func (a slimprotoPlayerAdapter) AddURLs(urls []string)     { a.p.AddURLs(urls) }
func (a slimprotoPlayerAdapter) Play() error               { return a.p.Play() }
func (a slimprotoPlayerAdapter) Pause() error              { return a.p.Pause() }
func (a slimprotoPlayerAdapter) Resume() error             { return a.p.Resume() }
func (a slimprotoPlayerAdapter) Stop() error               { return a.p.Stop() }

func (a slimprotoPlayerAdapter) GetState() slimproto.PlaybackState {
	switch a.p.GetState() {
	case player.StatePlaying:
		return slimproto.StatePlaying
	case player.StatePaused:
		return slimproto.StatePaused
	default:
		return slimproto.StateStopped
	}
}

func (a slimprotoPlayerAdapter) GetPlaybackTiming() *slimproto.PlaybackTiming {
	t := a.p.GetPlaybackTiming()
	if t == nil {
		return nil
	}
	return &slimproto.PlaybackTiming{
		Elapsed:   t.Elapsed,
		Duration:  t.Duration,
		Remaining: t.Remaining,
	}
}
