package main

import (
	"bufio"
	"expvar"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/golang/glog"
	"github.com/timpalpant/go-cfr"
	"github.com/timpalpant/go-cfr/mcts"
	"github.com/timpalpant/go-cfr/sampling"

	"github.com/timpalpant/alphacats"
	"github.com/timpalpant/alphacats/cards"
	"github.com/timpalpant/alphacats/gamestate"
)

var stdin = bufio.NewReader(os.Stdin)

var (
	gamesInProgress = expvar.NewInt("games_in_progress")
	gamesRemaining  = expvar.NewInt("games_remaining")
	numTraversals   = expvar.NewInt("num_traversals")
)

type RunParams struct {
	DeckType          string
	NumMCTSIterations int
	SamplingParams    SamplingParams
	Temperature       float64
}

type SamplingParams struct {
	Seed  int64
	C     float64
	Gamma float64
	Eta   float64
	D     float64
}

func getDeck(deckType string) (deck []cards.Card, cardsPerPlayer int) {
	switch deckType {
	case "test":
		deck = cards.TestDeck.AsSlice()
		cardsPerPlayer = (len(deck) / 2) - 1
	case "core":
		deck = cards.CoreDeck.AsSlice()
		cardsPerPlayer = 4
	default:
		panic(fmt.Errorf("unknown deck type: %v", deckType))
	}

	return deck, cardsPerPlayer
}

func main() {
	var params RunParams
	flag.StringVar(&params.DeckType, "decktype", "test", "Type of deck to use (core, test)")
	flag.IntVar(&params.NumMCTSIterations, "iter", 100, "Number of MCTS iterations to perform")
	flag.Float64Var(&params.Temperature, "temperature", 0.5,
		"Temperature used when selecting actions during play")
	flag.Int64Var(&params.SamplingParams.Seed, "sampling.seed", 123, "Random seed")
	flag.Float64Var(&params.SamplingParams.C, "sampling.c", 1.75,
		"Exploration factor C used in MCTS search")
	flag.Float64Var(&params.SamplingParams.Gamma, "sampling.gamma", 0.1,
		"Mixing factor Gamma used in Smooth UCT search")
	flag.Float64Var(&params.SamplingParams.Eta, "sampling.eta", 0.9,
		"Mixing factor eta used in Smooth UCT search")
	flag.Float64Var(&params.SamplingParams.D, "sampling.d", 0.001,
		"Mixing factor d used in Smooth UCT search")

	flag.Parse()

	rand.Seed(params.SamplingParams.Seed)
	go http.ListenAndServe("localhost:4123", nil)

	deck, cardsPerPlayer := getDeck(params.DeckType)
	optimizer := mcts.NewSmoothUCT(float32(params.SamplingParams.C),
		float32(params.SamplingParams.Gamma), float32(params.SamplingParams.Eta),
		float32(params.SamplingParams.D))
	for i := 0; ; i++ {
		deal := alphacats.NewRandomDeal(deck, cardsPerPlayer)
		playGame(optimizer, params, deal)
	}
}

func simulate(optimizer *mcts.SmoothUCT, beliefs *beliefState, n int) {
	p := normalizeProbabilities(beliefs.reachProbs)

	var wg sync.WaitGroup
	nWorkers := runtime.NumCPU()
	nPerWorker := n / nWorkers
	glog.Infof("Simulating %d games in %d workers", nWorkers*nPerWorker, nWorkers)
	for worker := 0; worker < nWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(rand.Int63()))
			for k := 0; k < nPerWorker; k++ {
				selected := sampling.SampleOne(p, rng.Float32())
				state := beliefs.states[selected]
				determinizedState := sampleDeterminization(state, rng)
				game := state.CloneWithState(determinizedState)
				optimizer.Run(game)
			}
		}()
	}

	wg.Wait()
}

func normalizeProbabilities(p []float32) []float32 {
	total := sum(p)
	result := make([]float32, len(p))
	for i, pi := range p {
		result[i] = pi / total
	}

	return result
}

func sum(vs []float32) float32 {
	var total float32
	for _, v := range vs {
		total += v
	}
	return total
}

func simulateRandomGames(optimizer *mcts.SmoothUCT, n int) {
	var wg sync.WaitGroup
	nWorkers := runtime.NumCPU()
	nPerWorker := n / nWorkers
	glog.Infof("Simulating %d games in %d workers", nWorkers*nPerWorker, nWorkers)
	for worker := 0; worker < nWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deck := cards.CoreDeck.AsSlice()
			for k := 0; k < nPerWorker; k++ {
				deal := alphacats.NewRandomDeal(deck, 4)
				game := alphacats.NewGame(deal.DrawPile, deal.P0Deal, deal.P1Deal)
				optimizer.Run(game)
			}
		}()
	}

	wg.Wait()

}

func playGame(policy *mcts.SmoothUCT, params RunParams, deal alphacats.Deal) {
	var game cfr.GameTreeNode = alphacats.NewGame(deal.DrawPile, deal.P0Deal, deal.P1Deal)
	simulateRandomGames(policy, params.NumMCTSIterations)

	glog.Infof("Building initial info set")
	beliefs := makeInitialBeliefState(deal)
	glog.Infof("Initial info set has %d game states", len(beliefs.states))

	for game.Type() != cfr.TerminalNodeType {
		if game.Type() == cfr.ChanceNodeType {
			var p float64
			game, p = game.SampleChild()
			glog.Infof("[chance] Sampled child node with probability %v", p)
			glog.Info("Propagating beliefs")
			beliefs = propagateBeliefs(policy, beliefs, game, float32(params.Temperature), true)
			glog.Infof("Infoset now has %d states", len(beliefs.states))
		} else if game.Player() == 0 {
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

			glog.Info("Propagating beliefs")
			beliefs = propagateBeliefs(policy, beliefs, game, float32(params.Temperature), true)
			glog.Infof("Infoset now has %d states", len(beliefs.states))
		} else {
			simulate(policy, beliefs, params.NumMCTSIterations)
			p := policy.GetPolicy(game, float32(params.Temperature))
			selected := sampling.SampleOne(p, rand.Float32())
			game = game.GetChild(selected)
			lastAction := game.(*alphacats.GameNode).LastAction()
			glog.Infof("[strategy] Chose to %v with probability %v: %v",
				hidePrivateInfo(lastAction), p[selected], p)
			glog.V(4).Infof("[strategy] Action result was: %v", lastAction)
			glog.Info("Propagating beliefs")
			beliefs = propagateBeliefs(policy, beliefs, game, float32(params.Temperature), false)
			glog.Infof("Infoset now has %d states", len(beliefs.states))
		}
	}

	glog.Info("GAME OVER")
	if game.Player() == 0 {
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

type beliefState struct {
	states     []*alphacats.GameNode
	reachProbs []float32
}

func propagateBeliefs(policy *mcts.SmoothUCT, bs *beliefState, actualGame cfr.GameTreeNode, temperature float32, inferredProb bool) *beliefState {
	actualIS := actualGame.(*alphacats.GameNode).GetInfoSet(gamestate.Player1)
	var states []*alphacats.GameNode
	var reachProbs []float32
	for i, game := range bs.states {
		// Determinize the next three cards so that all possible actions are concrete.
		for _, determinization := range enumerateDeterminizations(game) {
			for j := 0; j < determinization.NumChildren(); j++ {
				child := determinization.GetChild(j).(*alphacats.GameNode)
				is := child.GetInfoSet(gamestate.Player1)
				if is == actualIS {
					counterfactualP := float32(1.0)
					if inferredProb {
						policyP := policy.GetPolicy(determinization, temperature)
						counterfactualP = policyP[j]
					}

					// Determinized game is consistent with our observed history.
					states = append(states, child.Clone())
					reachProbs = append(reachProbs, counterfactualP*bs.reachProbs[i])
				}
			}
		}

		// If none of the children match, then this belief state is pruned as incompatible.
	}

	return &beliefState{states, reachProbs}
}

func enumerateDeterminizations(game *alphacats.GameNode) []*alphacats.GameNode {
	var result []*alphacats.GameNode
	// Determinize top 3 cards so that SeeTheFuture is fully specified.
	for _, determinizedState := range enumerateShuffleDeterminizations(game, 3) {
		// Determinize the bottom card so that DrawFromTheBottom is fully specified.
		drawPile := determinizedState.GetDrawPile()
		bottomCard := drawPile.NthCard(drawPile.Len() - 1)
		if bottomCard == cards.TBD {
			freeCards := getFreeCards(determinizedState)
			freeCards.Iter(func(card cards.Card, _ uint8) {
				drawPile.SetNthCard(drawPile.Len()-1, card)
				state := gamestate.NewShuffled(determinizedState, drawPile)
				determinizedGame := game.CloneWithState(state)
				result = append(result, determinizedGame)
			})
		} else {
			determinizedGame := game.CloneWithState(determinizedState)
			result = append(result, determinizedGame)
		}
	}
	return result
}

func sampleDeterminization(game *alphacats.GameNode, rng *rand.Rand) gamestate.GameState {
	state := game.GetState()
	freeCards := getFreeCards(state)
	freeCardsSlice := freeCards.AsSlice()
	rng.Shuffle(len(freeCardsSlice), func(i, j int) {
		freeCardsSlice[i], freeCardsSlice[j] = freeCardsSlice[j], freeCardsSlice[i]
	})

	drawPile := state.GetDrawPile()
	determinizedDrawPile := drawPile
	for i := 0; i < drawPile.Len(); i++ {
		nthCard := drawPile.NthCard(i)
		if nthCard != cards.TBD {
			continue
		}

		nextCard := freeCardsSlice[0]
		determinizedDrawPile.SetNthCard(i, nextCard)
		freeCardsSlice = freeCardsSlice[1:]
	}

	if len(freeCardsSlice) > 0 {
		panic(fmt.Errorf("Still have %d free cards remaining after determinization: %v", len(freeCardsSlice), freeCardsSlice))
	}

	determinizedState := gamestate.NewShuffled(state, determinizedDrawPile)
	return determinizedState
}

func getFreeCards(state gamestate.GameState) cards.Set {
	drawPile := state.GetDrawPile()
	p0Hand := state.GetPlayerHand(gamestate.Player0)
	p1Hand := state.GetPlayerHand(gamestate.Player1)
	h := state.GetHistory()

	freeCards := cards.CoreDeck
	freeCards.Add(cards.Defuse)
	freeCards.Add(cards.Defuse)
	freeCards.Add(cards.Defuse)
	freeCards.Add(cards.ExplodingKitten)

	// Remove all cards which are known to exist in either player's hand, a known position in the draw
	// pile, or have already been played.
	freeCards.RemoveAll(p0Hand)
	freeCards.RemoveAll(p1Hand)
	for i := 0; i < drawPile.Len(); i++ {
		nthCard := drawPile.NthCard(i)
		if nthCard != cards.Unknown && nthCard != cards.TBD {
			freeCards.Remove(nthCard)
		}
	}
	for i := 0; i < h.Len(); i++ {
		action := h.Get(i)
		if action.Type == gamestate.PlayCard {
			freeCards.Remove(action.Card)
		}
	}

	return freeCards
}

func enumerateShuffleDeterminizations(game *alphacats.GameNode, n int) []gamestate.GameState {
	state := game.GetState()
	drawPile := state.GetDrawPile()
	freeCards := getFreeCards(state)
	var result []gamestate.GameState
	seen := make(map[cards.Stack]struct{})
	enumerateShufflesHelper(freeCards, drawPile, n, func(determinizedDrawPile cards.Stack) {
		if _, ok := seen[determinizedDrawPile]; ok {
			return
		}

		seen[determinizedDrawPile] = struct{}{}
		determinizedState := gamestate.NewShuffled(state, determinizedDrawPile)
		result = append(result, determinizedState)
	})

	return result
}

func enumerateShufflesHelper(deck cards.Set, result cards.Stack, n int, cb func(shuffle cards.Stack)) {
	if n == 0 { // All cards have been used, complete shuffle.
		cb(result)
		return
	}

	nthCard := result.NthCard(n - 1)
	if nthCard == cards.TBD {
		deck.Iter(func(card cards.Card, count uint8) {
			// Take one of card from deck and append to result.
			remaining := deck
			remaining.Remove(card)
			newResult := result
			newResult.SetNthCard(n-1, card)

			// Recurse with remaining deck and new result.
			enumerateShufflesHelper(remaining, newResult, n-1, cb)
		})
	} else {
		enumerateShufflesHelper(deck, result, n-1, cb)
	}
}

// Return all game states consistent with player 1 initial deal.
func makeInitialBeliefState(deal alphacats.Deal) *beliefState {
	p1Hand := deal.P1Deal
	p1Hand.Remove(cards.Defuse)

	remaining := cards.CoreDeck
	remaining.RemoveAll(p1Hand)
	emptyDrawPile := cards.NewStack()
	for i := 0; i < deal.DrawPile.Len(); i++ {
		emptyDrawPile.SetNthCard(i, cards.TBD)
	}

	var states []*alphacats.GameNode
	seen := make(map[cards.Set]struct{})
	enumerateDealsHelper(remaining, cards.NewSet(), p1Hand.Len(), func(p0Hand cards.Set) {
		if _, ok := seen[p0Hand]; ok {
			return
		}

		seen[p0Hand] = struct{}{}
		p0Deal := p0Hand
		p0Deal.Add(cards.Defuse)
		p1Deal := p1Hand
		p1Deal.Add(cards.Defuse)
		game := alphacats.NewGame(emptyDrawPile, p0Deal, p1Deal)
		states = append(states, game)
	})

	return &beliefState{
		states:     states,
		reachProbs: uniformDistribution(len(states)),
	}
}

func uniformDistribution(n int) []float32 {
	result := make([]float32, n)
	for i := range result {
		result[i] = 1.0 / float32(n)
	}
	return result
}

func enumerateDealsHelper(deck cards.Set, result cards.Set, n int, cb func(deal cards.Set)) {
	if n == 0 {
		cb(result)
		return
	}

	deck.Iter(func(card cards.Card, count uint8) {
		remaining := deck
		remaining.Remove(card)
		newResult := result
		newResult.Add(card)
		enumerateDealsHelper(remaining, newResult, n-1, cb)
	})
}

func prompt(msg string) int {
	for {
		fmt.Print(msg)
		result, err := stdin.ReadString('\n')
		if err != nil {
			panic(err)
		}

		result = strings.TrimRight(result, "\n")
		i, err := strconv.Atoi(result)
		if err != nil {
			glog.Errorf("Invalid selection: %v", result)
			continue
		}

		return i
	}
}

func hidePrivateInfo(a gamestate.Action) gamestate.Action {
	a.PositionInDrawPile = 0
	a.CardsSeen = [3]cards.Card{}
	return a
}
