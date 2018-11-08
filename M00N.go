// Copyright 2015 The go-ethereum Authors
// Copyright 2015 Lefteris Karapetsas <lefteris@refu.co>
// Copyright 2015 Matthew Wampler-Doty <matthew.wampler.doty@gmail.com>
// Copyright 2018 Ngin project
// This file is part of Ngin.
//
// Ngin is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Ngin is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with Ngin. If not, see <http://www.gnu.org/licenses/>.

package M00N

/*
#cgo amd64 CFLAGS: -maes
#include "M00N.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/binary"
	"math/big"
	"math/rand"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/NginProject/ngind/common"
	"github.com/NginProject/ngind/core/types"
	"github.com/NginProject/ngind/logger"
	"github.com/NginProject/ngind/logger/glog"
	"github.com/NginProject/ngind/pow"
	//"github.com/klauspost/cpuid"
)

var (
	maxUint256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))
)

type M00N struct {
	hashRate int32
	turbo    bool
}

func (pow *M00N) GetHashrate() int64 {
	return int64(atomic.LoadInt32(&pow.hashRate))
}

func (pow *M00N) Search(block pow.Block, stop <-chan struct{}, index int) (nonce uint64) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	diff := block.Difficulty()

	i := int64(0)
	start := time.Now().UnixNano()
	previousHashrate := int32(0)

	nonce = uint64(r.Int63())
	target := new(big.Int).Div(maxUint256, diff)
	var ctx unsafe.Pointer = C.M00N_create()
	headerBytes := types.HeaderToBytes(block.Header())
	for {
		select {
		case <-stop:
			atomic.AddInt32(&pow.hashRate, -previousHashrate)
			C.M00N_destroy(ctx)
			return 0
		default:
			i++

			// we don't have to update hash rate on every nonce, so update after
			// first nonce check and then after 2^X nonces
			if i == 2 || ((i % (1 << 7)) == 0) {
				elapsed := time.Now().UnixNano() - start
				hashes := (float64(1e9) / float64(elapsed)) * float64(i)
				hashrateDiff := int32(hashes) - previousHashrate
				previousHashrate = int32(hashes)
				atomic.AddInt32(&pow.hashRate, hashrateDiff)
			}

			result := pow.compute(ctx, headerBytes, nonce).Big()

			// TODO: disagrees with the spec https://github.com/ethereum/wiki/wiki/Ethash#mining
			if result.Cmp(target) <= 0 {
				atomic.AddInt32(&pow.hashRate, -previousHashrate)
				C.M00N_destroy(ctx)
				return nonce
			}
			nonce += 1
		}

		if !pow.turbo {
			time.Sleep(20 * time.Microsecond)
		}
	}
}

func (pow *M00N) Turbo(on bool) {
	pow.turbo = on
}

func bytesToHash(in unsafe.Pointer) common.Hash {
	return *(*common.Hash)(in)
}

func (pow *M00N) compute(ctx unsafe.Pointer, blockBytes []byte, nonce uint64) common.Hash {
	binary.BigEndian.PutUint64(blockBytes[len(blockBytes)-8:], nonce)

	var in unsafe.Pointer = C.CBytes(blockBytes)
	var out unsafe.Pointer = C.malloc(common.HashLength)
	C.M00N_hash(ctx, (*C.char)(in), (*C.char)(out), C.uint32_t(len(blockBytes)))

	var hash common.Hash = bytesToHash(unsafe.Pointer(out))

	C.free(in)
	C.free(out)

	return hash
}

func (pow *M00N) CalcHash(headerBytes []byte, nonce uint64) *big.Int {
	var ctx unsafe.Pointer = C.M00N_create()
	result := pow.compute(ctx, headerBytes, nonce)
	C.M00N_destroy(ctx)

	return result.Big()
}

func (pow *M00N) Verify(block pow.Block) bool {
	difficulty := block.Difficulty()
	headerBytes := types.HeaderToBytes(block.Header())

	/* Cannot happen if block header diff is validated prior to PoW, but can
	happen if PoW is checked first due to parallel PoW checking.
	*/
	if difficulty.Cmp(common.Big0) == 0 {
		glog.V(logger.Debug).Infof("invalid block difficulty")
		return false
	}

	result := pow.CalcHash(headerBytes, block.Nonce())

	// The actual check.
	target := new(big.Int).Div(maxUint256, difficulty)
	return result.Cmp(target) <= 0
}

func New() *M00N {
	return &M00N{}
}

func NewShared() *M00N {
	return &M00N{}
}

func NewForTesting() (*M00N, error) {
	return &M00N{}, nil
}
