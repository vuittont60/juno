package migration

import (
	"bytes"
	"context"
	"testing"

	"github.com/NethermindEth/juno/blockchain"
	"github.com/NethermindEth/juno/clients/feeder"
	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/core/trie"
	"github.com/NethermindEth/juno/db"
	"github.com/NethermindEth/juno/db/pebble"
	"github.com/NethermindEth/juno/encoder"
	adaptfeeder "github.com/NethermindEth/juno/starknetdata/feeder"
	"github.com/NethermindEth/juno/utils"
	"github.com/bits-and-blooms/bitset"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration0000(t *testing.T) {
	testDB := pebble.NewMemTest(t)

	t.Run("empty DB", func(t *testing.T) {
		require.NoError(t, testDB.View(func(txn db.Transaction) error {
			return migration0000(txn, utils.MAINNET)
		}))
	})

	t.Run("non-empty DB", func(t *testing.T) {
		require.NoError(t, testDB.Update(func(txn db.Transaction) error {
			return txn.Set([]byte("asd"), []byte("123"))
		}))
		require.EqualError(t, testDB.View(func(txn db.Transaction) error {
			return migration0000(txn, utils.MAINNET)
		}), "initial DB should be empty")
	})
}

func TestRelocateContractStorageRootKeys(t *testing.T) {
	testDB := pebble.NewMemTest(t)

	txn := testDB.NewTransaction(true)

	numberOfContracts := 5

	// Populate the database with entries in the old location.
	for i := 0; i < numberOfContracts; i++ {
		exampleBytes := new(felt.Felt).SetUint64(uint64(i)).Bytes()
		// Use exampleBytes for the key suffix (the contract address) and the value.
		err := txn.Set(db.Unused.Key(exampleBytes[:]), exampleBytes[:])
		require.NoError(t, err)
	}

	require.NoError(t, relocateContractStorageRootKeys(txn, utils.MAINNET))

	// Each root-key entry should have been moved to its new location
	// and the old entry should not exist.
	for i := 0; i < numberOfContracts; i++ {
		exampleBytes := new(felt.Felt).SetUint64(uint64(i)).Bytes()

		// New entry exists.
		require.NoError(t, txn.Get(db.ContractStorage.Key(exampleBytes[:]), func(val []byte) error {
			require.Equal(t, exampleBytes[:], val, "the correct value was not transferred to the new location")
			return nil
		}))

		// Old entry does not exist.
		oldKey := db.Unused.Key(exampleBytes[:])
		err := txn.Get(oldKey, func(val []byte) error { return nil })
		require.ErrorIs(t, db.ErrKeyNotFound, err)
	}
}

func TestRecalculateBloomFilters(t *testing.T) {
	testdb := pebble.NewMemTest(t)
	chain := blockchain.New(testdb, utils.MAINNET, utils.NewNopZapLogger())
	client := feeder.NewTestClient(t, utils.MAINNET)
	gw := adaptfeeder.New(client)

	for i := uint64(0); i < 3; i++ {
		b, err := gw.BlockByNumber(context.Background(), i)
		require.NoError(t, err)
		su, err := gw.StateUpdate(context.Background(), i)
		require.NoError(t, err)

		b.EventsBloom = nil
		require.NoError(t, chain.Store(b, &core.BlockCommitments{}, su, nil))
	}

	require.NoError(t, testdb.Update(func(txn db.Transaction) error {
		return recalculateBloomFilters(txn, utils.MAINNET)
	}))

	for i := uint64(0); i < 3; i++ {
		b, err := chain.BlockByNumber(i)
		require.NoError(t, err)
		assert.Equal(t, core.EventsBloom(b.Receipts), b.EventsBloom)
	}
}

func TestChangeTrieNodeEncoding(t *testing.T) {
	testdb := pebble.NewMemTest(t)

	buckets := []db.Bucket{db.ClassesTrie, db.StateTrie, db.ContractStorage}

	var n struct {
		Value *felt.Felt
		Left  *bitset.BitSet
		Right *bitset.BitSet
	}
	require.NoError(t, testdb.Update(func(txn db.Transaction) error {
		// contract root keys, if changeTrieNodeEncoding tries to migrate these it
		// will fail with an error since they are not valid trie.Node encodings.
		require.NoError(t, txn.Set(db.ClassesTrie.Key(), []byte{1, 2, 3}))
		require.NoError(t, txn.Set(db.StateTrie.Key(), []byte{1, 2, 3}))
		require.NoError(t, txn.Set(db.ContractStorage.Key(make([]byte, felt.Bytes)), []byte{1, 2, 3}))

		for _, bucket := range buckets {
			for i := 0; i < 5; i++ {
				n.Value = new(felt.Felt).SetUint64(uint64(i))

				encodedNode, err := encoder.Marshal(n)
				if err != nil {
					return err
				}

				if err = txn.Set(bucket.Key([]byte{byte(i)}), encodedNode); err != nil {
					return err
				}
			}
		}

		return nil
	}))

	m := new(changeTrieNodeEncoding)
	m.Before()
	require.NoError(t, testdb.Update(func(txn db.Transaction) error {
		return m.Migrate(txn, utils.MAINNET)
	}))

	require.NoError(t, testdb.Update(func(txn db.Transaction) error {
		for _, bucket := range buckets {
			for i := 0; i < 5; i++ {
				var coreNode trie.Node
				err := txn.Get(bucket.Key([]byte{byte(i)}), coreNode.UnmarshalBinary)
				if err != nil {
					return err
				}
			}
		}

		return nil
	}))
}

func TestCalculateBlockCommitments(t *testing.T) {
	testdb := pebble.NewMemTest(t)
	chain := blockchain.New(testdb, utils.MAINNET, utils.NewNopZapLogger())
	client := feeder.NewTestClient(t, utils.MAINNET)
	gw := adaptfeeder.New(client)

	for i := uint64(0); i < 3; i++ {
		b, err := gw.BlockByNumber(context.Background(), i)
		require.NoError(t, err)
		su, err := gw.StateUpdate(context.Background(), i)
		require.NoError(t, err)
		require.NoError(t, chain.Store(b, &core.BlockCommitments{}, su, nil))
	}

	require.NoError(t, testdb.Update(func(txn db.Transaction) error {
		return calculateBlockCommitments(txn, utils.MAINNET)
	}))

	for i := uint64(0); i < 3; i++ {
		b, err := chain.BlockCommitmentsByNumber(i)
		require.NoError(t, err)
		assert.NotNil(t, b.TransactionCommitment)
	}
}

func TestMigrateTrieRootKeysFromBitsetToTrieKeys(t *testing.T) {
	memTxn := db.NewMemTransaction()

	bs := bitset.New(251)
	bsBytes, err := bs.MarshalBinary()
	require.NoError(t, err)

	key := []byte{0}
	err = memTxn.Set(key, bsBytes)
	require.NoError(t, err)

	require.NoError(t, migrateTrieRootKeysFromBitsetToTrieKeys(memTxn, key, bsBytes, utils.MAINNET))

	var trieKey trie.Key
	err = memTxn.Get(key, trieKey.UnmarshalBinary)
	require.NoError(t, err)
	require.Equal(t, bs.Len(), uint(trieKey.Len()))
	require.Equal(t, felt.Zero, trieKey.Felt())
}

func TestMigrateTrieNodesFromBitsetToTrieKey(t *testing.T) {
	migrator := migrateTrieNodesFromBitsetToTrieKey(db.ClassesTrie)
	memTxn := db.NewMemTransaction()

	bs := bitset.New(251)
	bsBytes, err := bs.MarshalBinary()
	require.NoError(t, err)

	n := node{
		Value: new(felt.Felt).SetUint64(123),
		Left:  bitset.New(37),
		Right: bitset.New(44),
	}

	var nodeBytes bytes.Buffer
	wrote, err := n._WriteTo(&nodeBytes)
	require.True(t, wrote > 0)
	require.NoError(t, err)

	nodeKey := db.ClassesTrie.Key(bsBytes)
	err = memTxn.Set(nodeKey, nodeBytes.Bytes())
	require.NoError(t, err)

	require.NoError(t, migrator(memTxn, nodeKey, nodeBytes.Bytes(), utils.MAINNET))

	err = memTxn.Get(db.ClassesTrie.Key(bsBytes), func(b []byte) error {
		return nil
	})
	require.ErrorIs(t, err, db.ErrKeyNotFound)

	var nodeKeyBuf bytes.Buffer
	newNodeKey := bitset2Key(bs)
	wrote, err = newNodeKey.WriteTo(&nodeKeyBuf)
	require.True(t, wrote > 0)
	require.NoError(t, err)

	var trieNode trie.Node
	err = memTxn.Get(db.Temporary.Key(nodeKeyBuf.Bytes()), trieNode.UnmarshalBinary)
	require.NoError(t, err)

	require.Equal(t, n.Value, trieNode.Value)
	require.Equal(t, n.Left.Len(), uint(trieNode.Left.Len()))
	require.Equal(t, n.Right.Len(), uint(trieNode.Right.Len()))
	require.Equal(t, felt.Zero, trieNode.Left.Felt())
	require.Equal(t, felt.Zero, trieNode.Right.Felt())
}
