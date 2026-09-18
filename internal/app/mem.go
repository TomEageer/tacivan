package app

import "runtime/debug"

// debugFreeOSMemory 把空闲堆页归还操作系统。
//
// Go 运行时默认会攒着不还，导致用户在活动监视器里看到内存「只涨不跌」。
// 关闭大结果集后主动还一次，用户看到的数字才和实际占用对得上。
func debugFreeOSMemory() {
	debug.FreeOSMemory()
}
