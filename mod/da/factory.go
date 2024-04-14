// SPDX-License-Identifier: MIT
//
// Copyright (c) 2024 Berachain Foundation
//
// Permission is hereby granted, free of charge, to any person
// obtaining a copy of this software and associated documentation
// files (the "Software"), to deal in the Software without
// restriction, including without limitation the rights to use,
// copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the
// Software is furnished to do so, subject to the following
// conditions:
//
// The above copyright notice and this permission notice shall be
// included in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
// EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
// OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
// HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.

package da

import (
	"github.com/berachain/beacon-kit/mod/config/params"
	"github.com/berachain/beacon-kit/mod/core/types"
	datypes "github.com/berachain/beacon-kit/mod/da/types"
	"github.com/berachain/beacon-kit/mod/merkle"
	engineprimitives "github.com/berachain/beacon-kit/mod/primitives-engine"
	"github.com/berachain/beacon-kit/mod/primitives/kzg"
	"golang.org/x/sync/errgroup"
)

// SidecarFactory is a factory for sidecars.
type SidecarFactory[BBB BeaconBlockBody] struct {
	cfg         *params.BeaconChainConfig
	kzgPosition uint64
}

// NewSidecarFactory creates a new sidecar factory.
func NewSidecarFactory[BBB BeaconBlockBody](
	cfg *params.BeaconChainConfig,
	// todo: calculate from config.
	kzgPosition uint64,
) *SidecarFactory[BBB] {
	return &SidecarFactory[BBB]{
		cfg: cfg,
		// TODO: This should be configurable / modular.
		kzgPosition: kzgPosition,
	}
}

// BuildSidecar builds a sidecar.
func (f *SidecarFactory[BBB]) BuildSidecars(
	blk BeaconBlock[BBB],
	blobs *engineprimitives.BlobsBundleV1,
) (*datypes.BlobSidecars, error) {
	numBlobs := uint64(len(blobs.Blobs))
	sidecars := make([]*datypes.BlobSidecar, numBlobs)
	body := blk.GetBody()
	g := errgroup.Group{}
	for i := range numBlobs {
		g.Go(func() error {
			inclusionProof, err := f.BuildKZGInclusionProof(
				body, i,
			)
			if err != nil {
				return err
			}
			blob := kzg.Blob(blobs.Blobs[i])
			sidecars[i] = datypes.BuildBlobSidecar(
				i,
				blk.GetHeader(),
				&blob,
				kzg.Commitment(blobs.Commitments[i]),
				kzg.Proof(blobs.Proofs[i]),
				inclusionProof,
			)
			return nil
		})
	}

	return &datypes.BlobSidecars{Sidecars: sidecars}, g.Wait()
}

// BuildKZGInclusionProof builds a KZG inclusion proof.
func (f *SidecarFactory[BBB]) BuildKZGInclusionProof(
	body BeaconBlockBody,
	index uint64,
) ([][32]byte, error) {
	// Build the merkle proof to the commitment within the
	// list of commitments.
	commitmentsProof, err := f.BuildCommitmentProof(body, index)
	if err != nil {
		return nil, err
	}

	// Build the merkle proof for the body root.
	bodyProof, err := f.BuildBlockBodyProof(body)
	if err != nil {
		return nil, err
	}

	// By property of the merkle tree, we can concatenate the
	// two proofs to get the final proof.
	return append(commitmentsProof, bodyProof...), nil
}

// BuildBlockBodyProof builds a block body proof.
func (f *SidecarFactory[BBB]) BuildBlockBodyProof(
	body BeaconBlockBody,
) ([][32]byte, error) {
	membersRoots, err := body.GetTopLevelRoots()
	if err != nil {
		return nil, err
	}
	tree, err := merkle.NewTreeWithMaxLeaves(
		membersRoots,
		uint64(types.BodyLengthDeneb),
	)
	if err != nil {
		return nil, err
	}

	topProof, err := tree.MerkleProof(f.kzgPosition)
	if err != nil {
		return nil, err
	}
	return topProof, nil
}

// BuildCommitmentProof builds a commitment proof.
func (f *SidecarFactory[BBB]) BuildCommitmentProof(
	body BeaconBlockBody,
	index uint64,
) ([][32]byte, error) {
	bodyTree, err := merkle.NewTreeWithMaxLeaves(
		body.GetBlobKzgCommitments().Leafify(),
		f.cfg.MaxBlobCommitmentsPerBlock,
	)
	if err != nil {
		return nil, err
	}

	return bodyTree.MerkleProofWithMixin(index)
}