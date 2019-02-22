package main

import (
	"compress/gzip"
	"flag"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/golang/glog"
	"github.com/timpalpant/go-cfr"

	"github.com/timpalpant/alphacats"
	"github.com/timpalpant/alphacats/cards"
)

func main() {
	seed := flag.Int64("seed", 123, "Random seed")
	iter := flag.Int("iter", 100000, "Number of iterations to perform")
	saveFile := flag.String("output", "", "File to save policy to")
	flag.Parse()

	rand.Seed(*seed)
	go http.ListenAndServe("localhost:4123", nil)

	opt := cfr.New(cfr.Params{
		SampleChanceNodes: true,
	})

	drawPile := cards.NewStackFromCards([]cards.Card{
		cards.Shuffle, cards.SeeTheFuture, cards.ExplodingCat,
		cards.Skip,
	})

	p0Deal := cards.NewSetFromCards([]cards.Card{
		cards.Defuse, cards.Skip, cards.Slap2x,
		cards.DrawFromTheBottom,
	})

	p1Deal := cards.NewSetFromCards([]cards.Card{
		cards.Defuse, cards.Slap1x, cards.Cat,
		cards.Skip,
	})

	var expectedValue float32
	for i := 0; i < *iter; i++ {
		rand.Shuffle(drawPile.Len(), func(i, j int) {
			tmp := drawPile.NthCard(i)
			drawPile.SetNthCard(i, drawPile.NthCard(j))
			drawPile.SetNthCard(j, tmp)
		})
		game := alphacats.NewGame(drawPile, p0Deal, p1Deal)
		expectedValue += opt.Run(game)
	}

	expectedValue /= float32(*iter)
	glog.Infof("Expected value is: %v", expectedValue)

	glog.Infof("Saving strategy to %v", *saveFile)
	f, err := os.Create(*saveFile)
	if err != nil {
		glog.Fatal(err)
	}
	defer f.Close()
	w := gzip.NewWriter(f)
	defer w.Close()
	if err := opt.Save(w); err != nil {
		glog.Fatal(err)
	}
}