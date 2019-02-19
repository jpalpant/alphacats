package alphacats

import (
	"fmt"
	"math/rand"

	"github.com/golang/glog"
	"github.com/timpalpant/go-cfr"

	"github.com/timpalpant/alphacats/cards"
	"github.com/timpalpant/alphacats/gamestate"
)

// turnType represents the kind of turn at a given point in the game.
type turnType uint8

const (
	_ turnType = iota
	PlayTurn
	GiveCard
	ShuffleDrawPile
	MustDefuse
	GameOver
)

var turnTypeStr = [...]string{
	"Invalid",
	"PlayTurn",
	"GiveCard",
	"ShuffleDrawPile",
	"MustDefuse",
	"GameOver",
}

func (tt turnType) String() string {
	return turnTypeStr[tt]
}

// GameNode implements cfr.GameTreeNode for Exploding Kittens.
// GameNode represents a state of play in the extensive-form game tree.
type GameNode struct {
	state    gamestate.GameState
	player   gamestate.Player
	turnType turnType
	// pendingTurns is the number of turns the player has outstanding
	// to play. In general this will be 1, except when Slap cards are played.
	pendingTurns int

	// children are the possible next states in the game.
	// Which child is realized will depend on chance or a player's action.
	children      []GameNode
	probabilities []float64

	gnPool *gameNodeSlicePool
	fPool  *floatSlicePool
}

// Verify that we implement the interface.
var _ cfr.GameTreeNode = &GameNode{}

func NewGame(drawPile cards.Stack, p0Deal, p1Deal cards.Set) *GameNode {
	return &GameNode{
		state: gamestate.New(drawPile, p0Deal, p1Deal),
		// Player0 always goes first.
		player:   gamestate.Player0,
		turnType: PlayTurn,
		gnPool:   &gameNodeSlicePool{},
		fPool:    &floatSlicePool{},
	}
}

func NewRandomGame() *GameNode {
	deck := cards.CoreDeck.AsSlice()
	rand.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})

	p0Deal := cards.NewSetFromCards(deck[:4])
	p0Deal.Add(cards.Defuse)
	p1Deal := cards.NewSetFromCards(deck[4:8])
	p1Deal.Add(cards.Defuse)
	drawPile := cards.NewStackFromCards(deck[8:])
	randPos := rand.Intn(drawPile.Len() + 1)
	drawPile.InsertCard(cards.ExplodingCat, randPos)
	return NewGame(drawPile, p0Deal, p1Deal)
}

// Type implements cfr.GameTreeNode.
func (gn *GameNode) Type() cfr.NodeType {
	switch gn.turnType {
	case ShuffleDrawPile:
		return cfr.ChanceNode
	case GameOver:
		return cfr.TerminalNode
	default:
		return cfr.PlayerNode
	}
}

// Player implements cfr.GameTreeNode.
func (gn *GameNode) Player() int {
	return int(gn.player)
}

// InfoSet implements cfr.GameTreeNode.
func (gn *GameNode) InfoSet(player int) string {
	return gn.state.GetInfoSet(gamestate.Player(player))
}

// Utility implements cfr.GameTreeNode.
func (gn *GameNode) Utility(player int) float64 {
	if gn.Type() != cfr.TerminalNode {
		panic("cannot get the utility of a non-terminal node")
	}

	if int(gn.player) == player {
		return 1.0
	}

	return -1.0
}

// String implements fmt.Stringer.
func (gn *GameNode) String() string {
	return fmt.Sprintf("%v's turn to %v. State: %s",
		gn.player, gn.turnType, gn.state.String())
}

func (gn *GameNode) allocChildren(n int) {
	if n > 1024 {
		glog.V(2).Infof("[%s] Allocating slice for %d children. State: %s",
			gn.turnType, n, gn.state.String())
	}
	gn.children = gn.gnPool.alloc(n)
	// Children are initialized as a copy of the current game node,
	// but without any children (the new node's children must be built).
	childPrototype := *gn
	childPrototype.children = nil
	childPrototype.probabilities = nil
	for i := range gn.children {
		gn.children[i] = childPrototype
	}

	if gn.Type() == cfr.ChanceNode {
		gn.probabilities = gn.fPool.alloc(n)
	}
}

// BuildChildren implements cfr.GameTreeNode.
func (gn *GameNode) BuildChildren() {
	if len(gn.children) > 0 {
		return // Already built.
	}

	switch gn.turnType {
	case PlayTurn:
		gn.buildPlayTurnChildren()
	case GiveCard:
		gn.buildGiveCardChildren()
	case ShuffleDrawPile:
		gn.buildShuffleChildren()
	case MustDefuse:
		gn.buildMustDefuseChildren()
	}
}

func (gn *GameNode) NumChildren() int {
	return len(gn.children)
}

// GetChild implements cfr.GameTreeNode.
func (gn *GameNode) GetChild(i int) cfr.GameTreeNode {
	return &gn.children[i]
}

// GetChildProbability implements cfr.GameTreeNode.
func (gn *GameNode) GetChildProbability(i int) float64 {
	return gn.probabilities[i]
}

// FreeChildren implements cfr.GameTreeNode.
func (gn *GameNode) FreeChildren() {
	gn.gnPool.free(gn.children)
	gn.children = nil
	gn.fPool.free(gn.probabilities)
	gn.probabilities = nil
}

func makePlayTurnNode(node *GameNode, player gamestate.Player, pendingTurns int) {
	if node.state.GetPlayerHand(player).Contains(cards.ExplodingCat) {
		// Player drew an exploding kitten, must defuse it before continuing.
		if node.state.GetPlayerHand(player).Contains(cards.Defuse) {
			// Player has a defuse card, must play it.
			makeMustDefuseNode(node, player, pendingTurns)
		} else {
			// Player does not have a defuse card, end game with loss for them.
			winner := nextPlayer(player)
			makeTerminalGameNode(node, winner)
		}
	} else {
		// Just a normal card, add it to player's hand and continue.
		if pendingTurns == 0 {
			// Player's turn is done, next player.
			player = nextPlayer(player)
			pendingTurns = 1
		}

		node.player = player
		node.turnType = PlayTurn
		node.pendingTurns = pendingTurns
	}
}

func makeGiveCardNode(node *GameNode, player gamestate.Player) {
	node.player = player
	node.turnType = GiveCard
}

func makeMustDefuseNode(node *GameNode, player gamestate.Player, pendingTurns int) {
	node.player = player
	node.turnType = MustDefuse
	node.pendingTurns = pendingTurns
	node.state.Apply(gamestate.Action{
		Player: player,
		Type:   gamestate.PlayCard,
		Card:   cards.Defuse,
	})
}

func makeTerminalGameNode(node *GameNode, winner gamestate.Player) {
	node.player = winner
	node.turnType = GameOver
}

func (gn *GameNode) buildPlayTurnChildren() {
	hand := gn.state.GetPlayerHand(gn.player)
	gn.allocChildren(hand.Len() + 1)
	i := 0
	// Play one of the cards in our hand.
	hand.Iter(func(card cards.Card, count uint8) {
		child := &gn.children[i]
		child.state.Apply(gamestate.Action{
			Player: gn.player,
			Type:   gamestate.PlayCard,
			Card:   card,
		})

		switch card {
		case cards.Defuse, cards.SeeTheFuture:
			makePlayTurnNode(child, gn.player, gn.pendingTurns)
		case cards.Skip, cards.DrawFromTheBottom:
			// Ends our current turn (with/without drawing a card).
			makePlayTurnNode(child, gn.player, gn.pendingTurns-1)
		case cards.Shuffle:
			child.turnType = ShuffleDrawPile
		case cards.Slap1x, cards.Slap2x:
			// Ends our turn (and all pending turns). Goes to next player with
			// any pending turns + slap.
			pendingTurns := 1
			if card == cards.Slap2x {
				pendingTurns = 2
			}

			if gn.state.LastActionWasSlap() { // Slap back.
				pendingTurns += gn.pendingTurns
			}

			makePlayTurnNode(child, nextPlayer(gn.player), pendingTurns)
		case cards.Cat:
			if child.state.GetPlayerHand(nextPlayer(gn.player)).Len() == 0 {
				// Other player has no cards in their hand, this was a no-op.
				makePlayTurnNode(child, gn.player, gn.pendingTurns)
			} else {
				// Other player must give us a card.
				makeGiveCardNode(child, nextPlayer(gn.player))
			}
		default:
			panic(fmt.Errorf("Player playing unsupported %v card", card))
		}

		i++
	})

	gn.children = gn.children[:i+1]
	// End our turn by drawing a card.
	lastChild := &gn.children[i]
	lastChild.state.Apply(gamestate.Action{
		Player: gn.player,
		Type:   gamestate.DrawCard,
	})
	makePlayTurnNode(lastChild, gn.player, gn.pendingTurns-1)
}

func (gn *GameNode) buildShuffleChildren() {
	drawPile := gn.state.GetDrawPile().ToSet()
	nShuffles := countDistinctShuffles(drawPile)
	gn.allocChildren(nShuffles)
	i := 0
	p := 1.0 / float64(nShuffles) // All shuffles are equally likely.
	enumerateShuffles(drawPile, func(shuffle cards.Stack) {
		child := &gn.children[i]
		child.state = gamestate.NewShuffled(child.state, shuffle)
		child.turnType = PlayTurn
		gn.probabilities[i] = p
		i++
	})
}

func (gn *GameNode) buildGiveCardChildren() {
	hand := gn.state.GetPlayerHand(gn.player)
	gn.allocChildren(hand.Len())
	i := 0
	hand.Iter(func(card cards.Card, count uint8) {
		// Form child node by:
		//   1) Removing card from our hand,
		//   2) Adding card to opponent's hand,
		//   3) Returning to opponent's turn.
		child := &gn.children[i]
		child.state.Apply(gamestate.Action{
			Player: gn.player,
			Type:   gamestate.GiveCard,
			Card:   card,
		})

		// Game play returns to other player (with the given card in their hand).
		makePlayTurnNode(child, nextPlayer(gn.player), gn.pendingTurns)

		i++
	})

	gn.children = gn.children[:i]
}

func (gn *GameNode) buildMustDefuseChildren() {
	nOptions := min(gn.state.GetDrawPile().Len(), 5)
	gn.allocChildren(nOptions + 1)
	for i := 0; i < nOptions; i++ {
		child := &gn.children[i]
		child.state.Apply(gamestate.Action{
			Player:             gn.player,
			Type:               gamestate.InsertExplodingCat,
			PositionInDrawPile: i,
		})

		// Defusing the exploding cat ends a turn.
		makePlayTurnNode(child, gn.player, gn.pendingTurns-1)
	}

	// Place exploding cat on the bottom of the draw pile.
	if gn.state.GetDrawPile().Len() > 5 {
		child := &gn.children[len(gn.children)-1]
		child.state.Apply(gamestate.Action{
			Player:             gn.player,
			Type:               gamestate.InsertExplodingCat,
			PositionInDrawPile: gn.state.GetDrawPile().Len(), // bottom
		})

		// Defusing the exploding cat ends a turn.
		makePlayTurnNode(child, gn.player, gn.pendingTurns-1)
	} else {
		gn.children = gn.children[:len(gn.children)-1]
	}

	// FIXME: Place randomly?
}

func nextPlayer(p gamestate.Player) gamestate.Player {
	if p != gamestate.Player0 && p != gamestate.Player1 {
		panic(fmt.Sprintf("cannot call nextPlayer with player %v", p))
	}

	return 1 - p
}

func min(i, j int) int {
	if i < j {
		return i
	}

	return j
}

func enumerateShuffles(deck cards.Set, cb func(shuffle cards.Stack)) {
	enumerateShufflesHelper(deck, cards.NewStack(), 0, cb)
}

func enumerateShufflesHelper(deck cards.Set, result cards.Stack, n int, cb func(shuffle cards.Stack)) {
	if deck.IsEmpty() { // All cards have been used, complete shuffle.
		cb(result)
		return
	}

	deck.Iter(func(card cards.Card, count uint8) {
		// Take one of card from deck and append to result.
		remaining := deck
		remaining.Remove(card)
		newResult := result
		newResult.InsertCard(card, n)
		// Recurse with remaining deck and new result.
		enumerateShufflesHelper(remaining, newResult, n+1, cb)
	})
}

func countDistinctShuffles(deck cards.Set) int {
	result := factorial(deck.Len())
	deck.Iter(func(card cards.Card, count uint8) {
		result /= factorial(int(count))
	})
	return result
}

func factorial(k int) int {
	result := 1
	for i := 2; i <= k; i++ {
		result *= i
	}
	return result
}
