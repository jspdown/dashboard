package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTx is a stand-in pgx.Tx used purely for identity: BeginTxFunc never
// calls any of its methods when joining an existing transaction.
type fakeTx struct{ pgx.Tx }

func TestBeginTxFunc_JoinsExistingTx(t *testing.T) {
	tx := &fakeTx{}
	parent := WithTx(context.Background(), tx)

	called := false
	err := BeginTxFunc(parent, nil, pgx.TxOptions{}, func(ctx context.Context) error {
		called = true
		got, inTx := TxFromContext(ctx)
		require.True(t, inTx, "fn must observe the existing transaction")
		assert.Same(t, pgx.Tx(tx), got, "joined tx must be the same instance")
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called, "fn must be invoked even when joining")
}

func TestBeginTxFunc_JoinedInnerErrorPropagates(t *testing.T) {
	parent := WithTx(context.Background(), &fakeTx{})
	want := errors.New("boom")

	err := BeginTxFunc(parent, nil, pgx.TxOptions{}, func(_ context.Context) error {
		return want
	})

	assert.ErrorIs(t, err, want)
}
