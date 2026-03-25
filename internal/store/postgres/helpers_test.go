package postgres_test

import (
	"context"
	"log"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/cca/go-indexer/internal/domain/cca"
	"github.com/cca/go-indexer/internal/store"
	"github.com/cca/go-indexer/internal/store/postgres"
	"github.com/ethereum/go-ethereum/common"
)

var (
	testStore   store.Store
	testPool    *pgxpool.Pool
	pgContainer testcontainers.Container
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		log.Fatalf("failed to start postgres container: %v", err)
	}
	pgContainer = container

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("failed to get connection string: %v", err)
	}

	s, err := postgres.New(ctx, connStr)
	if err != nil {
		log.Fatalf("failed to create store: %v", err)
	}
	testStore = s

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		s.Close()
		log.Fatalf("failed to create verification pool: %v", err)
	}
	testPool = pool

	code := m.Run()

	pool.Close()
	s.Close()
	if err := container.Terminate(ctx); err != nil {
		log.Printf("failed to terminate container: %v", err)
	}

	os.Exit(code)
}

func truncateAll(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := testPool.Exec(ctx, "TRUNCATE cursors, blocks, raw_events, auctions")
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
}

func newTestAuction(blockNumber uint64) *cca.Auction {
	return &cca.Auction{
		AuctionAddress:        common.HexToAddress("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
		Token:                 common.HexToAddress("0xBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"),
		TotalSupply:           big.NewInt(1_000_000),
		Currency:              common.HexToAddress("0xCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"),
		TokensRecipient:       common.HexToAddress("0xDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"),
		FundsRecipient:        common.HexToAddress("0xEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE"),
		StartBlock:            100,
		EndBlock:              200,
		ClaimBlock:            250,
		TickSpacingQ96:        new(big.Int).Lsh(big.NewInt(1), 96),
		ValidationHook:        common.HexToAddress("0x0000000000000000000000000000000000000000"),
		FloorPriceQ96:         func() *big.Int { v, _ := new(big.Int).SetString("7922816251426433759354395034", 10); return v }(),
		RequiredCurrencyRaised: big.NewInt(500_000),
		AuctionStepsData:      []byte{0x01, 0x02, 0x03},
		EmitterContract:       common.HexToAddress("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"),
		ChainID:               324,
		BlockNumber:           blockNumber,
		TxHash:                common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
		LogIndex:              0,
		CreatedAt:             time.Now().UTC().Truncate(time.Microsecond),
	}
}

func newTestRawEvent(blockNumber uint64) *cca.RawEvent {
	return &cca.RawEvent{
		ChainID:     324,
		BlockNumber: blockNumber,
		BlockHash:   common.HexToHash("0xblockhash5"),
		TxHash:      common.HexToHash("0xtxhash5"),
		LogIndex:    0,
		Address:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		EventName:   "AuctionCreated",
		TopicsJSON:  `["0xtopic0","0xtopic1"]`,
		DataHex:     "0xdeadbeef",
		DecodedJSON: `{"key":"value"}`,
		IndexedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
}
