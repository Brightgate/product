//
// Copyright 2017 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//


package cloud_rpc

// These eight byte keys were generated using
//
//    od -N 1000 -x /dev/urandom
//
// on recent x86_64 Linux and selecting various rows.
var (
	HMACKeys = map[int][]byte{
		2017: []byte{0x73, 0xca, 0x63, 0x30, 0x9f, 0xed, 0x89, 0x6f},
		2018: []byte{0x13, 0xba, 0xd0, 0x65, 0x4e, 0xe1, 0xc4, 0x5e},
		2019: []byte{0x04, 0x97, 0xc4, 0x67, 0xd4, 0xa2, 0xd7, 0xe2},
		2020: []byte{0xfc, 0x52, 0xed, 0x28, 0x17, 0xee, 0xe6, 0x07},
	}
)

