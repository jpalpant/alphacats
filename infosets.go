package alphacats

import (
	"fmt"

	"github.com/timpalpant/alphacats/cards"
)

// InfoSet defines a minimal and hashable representation of a Player's
// current state of knowledge within a game, including that of:
//   1) The cards in our hand,
//   2) Any cards we know to be in the other player's hand,
//   3) The cards in the draw pile, and their positions if we know it,
//   4) The outstanding set of cards that must be in play somewhere,
//      but whose location we do not yet know.
//
// Because an InfoSet completely defines a player's point of view
// within the game, a Player strategy must have a single action policy
// for each distinct InfoSet.
type InfoSet struct {
	// The Cards we have in our hand. All Cards should be known.
	OurHand cards.Set
	// The Cards our opponent has in their hand. Some Cards may be Unknown.
	OpponentHand cards.Set
	// The Cards remaining in the draw pile. Some Cards may be Unknown.
	DrawPile cards.Set
	// Cards that we know in the draw pile. For example, after playing a
	// SeeTheFuture card we know the identity of the top three cards.
	KnownDrawPileCards cards.Stack
	// The remaining Cards whose location we do not know. These may be
	// in our opponent's hand or in the draw pile. The number of cards should
	// correspond to the total number of Unknown Cards in the OpponentHand
	// and the DrawPile.
	RemainingCards cards.Set
}

// Verifies that the InfoSet is valid and satisifes all internal constraints.
func (is InfoSet) Validate() error {
	// All cards in our hand must be known.
	if is.OurHand.CountOf(cards.Unknown) != 0 {
		return fmt.Errorf("found Unknown cards in our hand")
	}

	// Number of remaining cards must equal number of Unknowns
	// in draw pile + opponent hand.
	unknownCards := is.OpponentHand
	unknownCards.AddAll(is.DrawPile)
	nUnknown := unknownCards.CountOf(cards.Unknown)
	if int(nUnknown) != is.RemainingCards.Len() {
		return fmt.Errorf("%d remaining cards but %d Unknowns", is.RemainingCards.Len(), nUnknown)
	}

	// Any known cards in the draw pile must exist in the draw pile CardSet.
	for i := 0; i < is.DrawPile.Len(); i++ {
		card := is.KnownDrawPileCards.NthCard(i)
		if card != cards.Unknown {
			if is.DrawPile.CountOf(card) == 0 {
				return fmt.Errorf("%v in draw pile but not card set", card)
			}
		}
	}

	return nil
}

// Return a new InfoSet created as if the player is dealt the given
// Set of (4) cards at the beginning of the game, not including the
// Defuse card that is always added.
func NewInfoSetFromInitialDeal(deal cards.Set) InfoSet {
	if deal.Len() != 4 {
		panic(fmt.Errorf("initial deal must have 4 cards, got %d", deal.Len()))
	}

	ourHand := deal
	ourHand[cards.Defuse] += 1

	opponentHand := cards.Set{}
	opponentHand[cards.Defuse] = 1
	opponentHand[cards.Unknown] = 4

	drawPile := cards.Set{}
	drawPile[cards.ExplodingCat] = 1
	drawPile[cards.Unknown] = 12

	remainingCards := cards.CoreDeck
	remainingCards.RemoveAll(deal)

	return InfoSet{
		OurHand:        ourHand,
		OpponentHand:   opponentHand,
		DrawPile:       drawPile,
		RemainingCards: remainingCards,
	}
}

// Modify InfoSet as if we drew the given Card from the top of the draw pile.
func (is *InfoSet) DrawCard(card cards.Card, fromBottom bool) {
	// Add card to our hand.
	is.OurHand[card]++

	position := 0
	if fromBottom {
		position = is.DrawPile.Len() - 1
	}

	// NB: drawnCard may be unknown (it is a random variable
	// realized as the card passed as an argument).
	drawnCard := is.KnownDrawPileCards.NthCard(position)
	// Shift our known draw pile cards up by one.
	is.KnownDrawPileCards.RemoveCard(position)
	is.DrawPile[drawnCard]--
	// If we didn't know what the card in the pile was already, we know now.
	if drawnCard == cards.Unknown {
		is.RemainingCards[card]--
	}
}

func (is *InfoSet) PlayCard(card cards.Card) {
	is.OurHand[card]--
}

// Modify InfoSet to reflect our opponent drawing the top card
// of the draw pile.
func (is *InfoSet) OpponentDrewCard(fromBottom bool) {
	position := 0
	if fromBottom {
		position = is.DrawPile.Len() - 1
	}

	// If we knew what the card in the pile was, we now know it is in their hand.
	drawnCard := is.KnownDrawPileCards.NthCard(position)
	is.KnownDrawPileCards.RemoveCard(position)
	is.OpponentHand[drawnCard]++
	is.DrawPile[drawnCard]--
}

func (is *InfoSet) OpponentPlayedCard(card cards.Card) {
	if is.OpponentHand.CountOf(card) > 0 {
		// We knew the player had this card.
		is.OpponentHand[card]--
	} else {
		is.OpponentHand[cards.Unknown]--
		is.RemainingCards[card]--
	}
}

// Modify InfoSet to reflect seeing these cards on the top
// of the draw pile.
func (is *InfoSet) SeeTopCards(topN []cards.Card) {
	for i, card := range topN {
		// Did we know this card already?
		nthCard := is.KnownDrawPileCards.NthCard(i)
		if nthCard == cards.Unknown {
			// Now we know it.
			is.RemainingCards[card]--                 // No longer an Unknown card somewhere.
			is.DrawPile[nthCard]--                    // No longer Unknown.
			is.DrawPile[card]++                       // Now we know what it is.
			is.KnownDrawPileCards.SetNthCard(i, card) // And where it is.
		} else if nthCard != card {
			panic(fmt.Errorf("we knew %d th card to be %v, but are now told it is %v",
				i, nthCard, card))
		}
	}
}
