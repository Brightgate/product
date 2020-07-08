//
// COPYRIGHT 2017 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or  alteration will be a violation of federal law.
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
