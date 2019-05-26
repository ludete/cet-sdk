package market

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Bankx Keeper will implement the interface
type ExpectedBankxKeeper interface {
	HaveSufficientCoins(addr sdk.AccAddress, amt sdk.Coins) bool                            // to check whether have sufficient coins in special address
	SendCoins(from, to sdk.AccAddress, amt sdk.Coins) error                                 // to tranfer coins
	FreezeCoins(acc sdk.AccAddress, amt sdk.Coins) error                                    // freeze some coins when creating orders
	UnfreezeCoins(acc sdk.AccAddress, amt sdk.Coins) error                                  // unfreeze coins and then orders can be executed
	DeductFeeFromAddressAndCollectFeetoIncentive(acc sdk.AccAddress, coins sdk.Coins) error // Deduct the address balance and deliver the deducted amount to the incentive module
}

// Asset Keeper will implement the interface
type ExpectedAssertStatusKeeper interface {
	IsTokenFrozen(addr sdk.AccAddress, denom string) bool // the coin's issuer has frozen "denom", forbiding transmission and exchange.
	IsTokenExists(denom string) bool                      // check whether there is a coin named "denom"
	IsTokenIssuer(denom string, addr sdk.AccAddress) bool
}