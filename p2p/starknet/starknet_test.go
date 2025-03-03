package starknet_test

import (
	"context"
	"testing"

	"github.com/NethermindEth/juno/mocks"
	"github.com/NethermindEth/juno/p2p/starknet"
	"github.com/NethermindEth/juno/p2p/starknet/spec"
	"github.com/NethermindEth/juno/utils"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestClientHandler(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	testNetwork := utils.INTEGRATION
	testCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mockNet, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	peers := mockNet.Peers()
	require.Len(t, peers, 2)
	handlerID := peers[0]
	clientID := peers[1]

	log := utils.NewNopZapLogger()
	mockReader := mocks.NewMockReader(mockCtrl)
	handler := starknet.NewHandler(mockReader, log)

	handlerHost := mockNet.Host(handlerID)
	handlerHost.SetStreamHandler(starknet.BlockHeadersPID(testNetwork), handler.BlockHeadersHandler)
	handlerHost.SetStreamHandler(starknet.BlockBodiesPID(testNetwork), handler.BlockBodiesHandler)
	handlerHost.SetStreamHandler(starknet.EventsPID(testNetwork), handler.EventsHandler)
	handlerHost.SetStreamHandler(starknet.ReceiptsPID(testNetwork), handler.ReceiptsHandler)
	handlerHost.SetStreamHandler(starknet.TransactionsPID(testNetwork), handler.TransactionsHandler)

	clientHost := mockNet.Host(clientID)
	client := starknet.NewClient(func(ctx context.Context, pids ...protocol.ID) (network.Stream, error) {
		return clientHost.NewStream(ctx, handlerID, pids...)
	}, testNetwork, log)

	t.Run("get block headers", func(t *testing.T) {
		res, cErr := client.RequestBlockHeaders(testCtx, &spec.BlockHeadersRequest{})
		require.NoError(t, cErr)

		count := uint64(0)
		for header, valid := res(); valid; header, valid = res() {
			assert.Equal(t, count, header.GetPart()[0].GetHeader().Number)
			count++
		}
		require.Equal(t, uint64(4), count)
	})

	t.Run("get block bodies", func(t *testing.T) {
		res, cErr := client.RequestBlockBodies(testCtx, &spec.BlockBodiesRequest{})
		require.NoError(t, cErr)

		count := uint64(0)
		for body, valid := res(); valid; body, valid = res() {
			assert.Equal(t, count, body.Id.Number)
			count++
		}
		require.Equal(t, uint64(4), count)
	})

	t.Run("get receipts", func(t *testing.T) {
		res, cErr := client.RequestReceipts(testCtx, &spec.ReceiptsRequest{})
		require.NoError(t, cErr)

		count := uint64(0)
		for receipt, valid := res(); valid; receipt, valid = res() {
			assert.Equal(t, count, receipt.Id.Number)
			count++
		}
		require.Equal(t, uint64(4), count)
	})

	t.Run("get txns", func(t *testing.T) {
		res, cErr := client.RequestTransactions(testCtx, &spec.TransactionsRequest{})
		require.NoError(t, cErr)

		count := uint64(0)
		for txn, valid := res(); valid; txn, valid = res() {
			assert.Equal(t, count, txn.Id.Number)
			count++
		}
		require.Equal(t, uint64(4), count)
	})

	t.Run("get events", func(t *testing.T) {
		res, cErr := client.RequestEvents(testCtx, &spec.EventsRequest{})
		require.NoError(t, cErr)

		count := uint64(0)
		for evnt, valid := res(); valid; evnt, valid = res() {
			assert.Equal(t, count, evnt.Id.Number)
			count++
		}
		require.Equal(t, uint64(4), count)
	})
}
