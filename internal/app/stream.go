package app

import (
	"context"

	"tacivan/internal/dbx"
)

// cancelStream 把一个 context 的取消函数绑到行流上，流关闭时一并取消。
type cancelStream struct {
	dbx.RowStream
	cancel context.CancelFunc
}

func (s *cancelStream) Close() error {
	err := s.RowStream.Close()
	s.cancel()
	return err
}

// Cancel 掐断底层查询的 context，让卡在 Next() 上的读取立刻返回。
//
// 这是「停止」按钮能成立的关键：Close 必须等当前 Next 返回才安全，
// 而用户要停的恰恰就是那个迟迟不返回的 Next。
func (s *cancelStream) Cancel() { s.cancel() }

// longLivedQuery 执行一个「结果集要活过本次请求」的查询。
//
// 这是流式取数能成立的前提：如果查询用请求级的带超时 context，
// 函数一返回 defer cancel() 就会掐断底层游标，用户往后翻页时只会拿到空结果。
//
// 这里刻意不去联动调用方 context 的取消：之前试过用 select 在
// 「ctx 结束」和「查询建立完成」之间二选一，但两者同时就绪时 select 随机挑一个，
// 于是有一半概率把刚建好的游标误杀。建立阶段的超时保护交给驱动层的
// 连接/读写超时（那本来就是网络层该管的事），停止查询则由调用方关闭结果集完成。
func longLivedQuery(
	ctx context.Context,
	run func(context.Context) (dbx.RowStream, error),
) (dbx.RowStream, error) {
	qctx, qcancel := context.WithCancel(context.WithoutCancel(ctx))
	stream, err := run(qctx)
	if err != nil {
		qcancel()
		return nil, err
	}
	return &cancelStream{RowStream: stream, cancel: qcancel}, nil
}
