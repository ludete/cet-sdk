package types

import (
	"encoding/json"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client/context"
	"github.com/cosmos/cosmos-sdk/codec"
)

type mockModuleBasic struct {
	name            string
	restRoutesOrder *string
	txCmdsOrder     *string
	queryCmdsOrder  *string
}

func (m mockModuleBasic) Name() string {
	return m.name
}

func (m mockModuleBasic) RegisterCodec(*codec.Codec) {

}

// genesis
func (m mockModuleBasic) DefaultGenesis() json.RawMessage {
	return nil
}
func (m mockModuleBasic) ValidateGenesis(json.RawMessage) error {
	return nil
}

// client functionality
func (m mockModuleBasic) RegisterRESTRoutes(context.CLIContext, *mux.Router) {
	*(m.restRoutesOrder) += m.name
}
func (m mockModuleBasic) GetTxCmd(*codec.Codec) *cobra.Command {
	*(m.txCmdsOrder) += m.name
	return &cobra.Command{}
}
func (m mockModuleBasic) GetQueryCmd(*codec.Codec) *cobra.Command {
	*(m.queryCmdsOrder) += m.name
	return &cobra.Command{}
}

func TestOrders(t *testing.T) {
	restRoutesOrder := ""
	txCmdsOrder := ""
	queryCmdsOrder := ""
	newMockModuleBasic := func(name string) mockModuleBasic {
		return mockModuleBasic{
			name:            name,
			restRoutesOrder: &restRoutesOrder,
			txCmdsOrder:     &txCmdsOrder,
			queryCmdsOrder:  &queryCmdsOrder,
		}
	}

	ma := newMockModuleBasic("a")
	mb := newMockModuleBasic("b")
	mc := newMockModuleBasic("c")
	obm := NewOrderedBasicManager([]module.AppModuleBasic{mc, ma, mb})

	obm.RegisterRESTRoutes(context.CLIContext{}, nil)
	require.Equal(t, "cab", restRoutesOrder)

	obm.AddTxCommands(&cobra.Command{}, nil)
	require.Equal(t, "cab", txCmdsOrder)

	obm.AddQueryCommands(&cobra.Command{}, nil)
	require.Equal(t, "cab", queryCmdsOrder)
}
