package legs_test

import (
	"context"
	"log"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/filecoin-project/go-legs"
	"github.com/filecoin-project/go-legs/dtsync"
	"github.com/filecoin-project/go-legs/test"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	_ "github.com/ipld/go-ipld-prime/codec/dagcbor"
	_ "github.com/ipld/go-ipld-prime/codec/dagjson"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	basicnode "github.com/ipld/go-ipld-prime/node/basic"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
)

func TestMain(m *testing.M) {
	if runtime.GOARCH == "386" {
		log.Println("Skipping tests, cannot use GOARCH=386")
		return
	}

	// Run tests.
	os.Exit(m.Run())
}

func initPubSub(t *testing.T, srcStore, dstStore datastore.Batching) (host.Host, host.Host, legs.Publisher, *legs.Subscriber, error) {
	srcHost := test.MkTestHost()
	dstHost := test.MkTestHost()

	topics := test.WaitForMeshWithMessage(t, testTopic, srcHost, dstHost)

	srcLnkS := test.MkLinkSystem(srcStore)

	pub, err := dtsync.NewPublisher(srcHost, srcStore, srcLnkS, testTopic, dtsync.Topic(topics[0]), dtsync.WithExtraData([]byte("t01000")))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	srcHost.Peerstore().AddAddrs(dstHost.ID(), dstHost.Addrs(), time.Hour)
	dstHost.Peerstore().AddAddrs(srcHost.ID(), srcHost.Addrs(), time.Hour)
	dstLnkS := test.MkLinkSystem(dstStore)

	sub, err := legs.NewSubscriber(dstHost, dstStore, dstLnkS, testTopic, nil, legs.Topic(topics[1]))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if err := srcHost.Connect(context.Background(), dstHost.Peerstore().PeerInfo(dstHost.ID())); err != nil {
		return nil, nil, nil, nil, err
	}

	return srcHost, dstHost, pub, sub, nil
}

func TestAllowPeerReject(t *testing.T) {
	// Init legs publisher and subscriber
	srcStore := dssync.MutexWrap(datastore.NewMapDatastore())
	dstStore := dssync.MutexWrap(datastore.NewMapDatastore())
	_, dstHost, pub, sub, err := initPubSub(t, srcStore, dstStore)
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()
	defer sub.Close()

	// Set function to reject anything except dstHost, which is not the one
	// generating the update.
	sub.SetAllowPeer(func(peerID peer.ID) (bool, error) {
		return peerID == dstHost.ID(), nil
	})

	watcher, cncl := sub.OnSyncFinished()
	defer cncl()

	c := mkLnk(t, srcStore)

	// Update root with item
	err = pub.UpdateRoot(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-time.After(time.Second * 3):
	case _, open := <-watcher:
		if open {
			t.Fatal("something was exchanged, and that is wrong")
		}
	}
}

func TestAllowPeerAllows(t *testing.T) {
	// Init legs publisher and subscriber
	srcStore := dssync.MutexWrap(datastore.NewMapDatastore())
	dstStore := dssync.MutexWrap(datastore.NewMapDatastore())
	_, _, pub, sub, err := initPubSub(t, srcStore, dstStore)
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()
	defer sub.Close()

	// Set function to allow any peer.
	sub.SetAllowPeer(func(peerID peer.ID) (bool, error) {
		return true, nil
	})

	watcher, cncl := sub.OnSyncFinished()
	defer cncl()

	c := mkLnk(t, srcStore)

	// Update root with item
	err = pub.UpdateRoot(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-time.After(updateTimeout):
		t.Fatal("timed out waiting for SyncFinished")
	case <-watcher:
	}
}

func mkLnk(t *testing.T, srcStore datastore.Batching) cid.Cid {
	// Update root with item
	np := basicnode.Prototype__Any{}
	nb := np.NewBuilder()
	ma, _ := nb.BeginMap(2)
	err := ma.AssembleKey().AssignString("hey")
	if err != nil {
		t.Fatal(err)
	}
	if err = ma.AssembleValue().AssignString("it works!"); err != nil {
		t.Fatal(err)
	}
	if err = ma.AssembleKey().AssignString("yes"); err != nil {
		t.Fatal(err)
	}
	if err = ma.AssembleValue().AssignBool(true); err != nil {
		t.Fatal(err)
	}
	if err = ma.Finish(); err != nil {
		t.Fatal(err)
	}
	n := nb.Build()
	lnk, err := test.Store(srcStore, n)
	if err != nil {
		t.Fatal(err)
	}

	return lnk.(cidlink.Link).Cid
}
