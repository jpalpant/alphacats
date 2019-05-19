package main

import (
	"bufio"
	"encoding/gob"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"

	"github.com/golang/glog"
	gzip "github.com/klauspost/pgzip"
	"github.com/timpalpant/go-cfr"
	"github.com/timpalpant/go-cfr/sampling"

	"github.com/timpalpant/alphacats"
	"github.com/timpalpant/alphacats/cards"
	"github.com/timpalpant/alphacats/gamestate"
	_ "github.com/timpalpant/alphacats/model"
)

var stdin = bufio.NewReader(os.Stdin)

func main() {
	strat := flag.String("strategy", "", "File with strategy to play against")
	player := flag.Int("player", 0, "Player to play as")
	seed := flag.Int64("seed", 1234, "Random seed")
	flag.Parse()

	rand.Seed(*seed)
	go http.ListenAndServe("localhost:4123", nil)

	deck := cards.TestDeck.AsSlice()
	cardsPerPlayer := (len(deck) / 2) - 1
	policy := mustLoadPolicy(*strat)
	var game cfr.GameTreeNode = alphacats.NewRandomGame(deck, cardsPerPlayer)

	for game.Type() != cfr.TerminalNodeType {
		if game.Type() == cfr.ChanceNodeType {
			var p float64
			game, p = game.SampleChild()
			glog.Infof("[chance] Sampled child node with probability %v", p)
		} else if game.Player() == *player {
			is := game.InfoSet(game.Player()).(*alphacats.InfoSetWithAvailableActions)
			glog.Infof("[player] Your turn. %d cards remaining in draw pile.",
				game.(*alphacats.GameNode).GetDrawPile().Len())
			glog.Infof("[player] Hand: %v, Choices:", is.InfoSet.Hand)
			for i, action := range is.AvailableActions {
				glog.Infof("%d: %v", i, action)
			}

			selected := prompt("Which action? ")
			game = game.GetChild(selected)
			lastAction := game.(*alphacats.GameNode).LastAction()
			glog.Infof("[player] Chose to %v", lastAction)
		} else {
			p := policy.GetPolicy(game).GetAverageStrategy()
			selected := sampling.SampleOne(p, rand.Float32())
			game = game.GetChild(selected)
			lastAction := game.(*alphacats.GameNode).LastAction()
			glog.Infof("[strategy] Chose to %v with probability %v: %v",
				hidePrivateInfo(lastAction), p[selected], p)
			glog.V(4).Infof("[strategy] Action result was: %v", lastAction)
		}
	}

	glog.Info("GAME OVER")
	if game.Player() == *player {
		glog.Info("You win!")
	} else {
		glog.Info("Computer wins!")
	}

	glog.Info("Game history:")
	h := game.(*alphacats.GameNode).GetHistory()
	for i, action := range h.AsSlice() {
		glog.Infof("%d: %v", i, action)
	}
}

func hidePrivateInfo(a gamestate.Action) gamestate.Action {
	a.PositionInDrawPile = 0
	a.CardsSeen = [3]cards.Card{}
	return a
}

func mustLoadPolicy(filename string) cfr.StrategyProfile {
	glog.Infof("Loading strategy from: %v", filename)
	f, err := os.Open(filename)
	if err != nil {
		glog.Fatal(err)
	}
	defer f.Close()

	r, err := gzip.NewReader(f)
	if err != nil {
		glog.Fatal(err)
	}

	var policy cfr.StrategyProfile
	dec := gob.NewDecoder(r)
	if err := dec.Decode(&policy); err != nil {
		glog.Fatal(err)
	}

	return policy
}

func prompt(msg string) int {
	fmt.Print(msg)
	result, err := stdin.ReadString('\n')
	if err != nil {
		panic(err)
	}

	result = strings.TrimRight(result, "\n")
	i, err := strconv.Atoi(result)
	if err != nil {
		panic(err)
	}

	return i
}
