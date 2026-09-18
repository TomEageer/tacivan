// Package rs 实现结果集引擎：流式游标 + 分块内存缓存 + 磁盘溢出 + 全局内存预算。
//
// 这是本项目相对传统桌面客户端的核心改进点。它们打开大表或执行宽结果集查询时，
// 往往把全部行读进进程内存，行数一大就直接把内存吃穿。这里的做法是：
//
//  1. 驱动只提供单向流，任何时候都不持有全量数据；
//  2. 行按块（chunk）缓存在内存，块数受全局预算约束，最久未用的块可被丢弃；
//  3. 内存超过阈值后启用磁盘溢出，被丢弃的块随时能从溢出文件重建；
//  4. 单元格大值在驱动侧就已截断，完整值按主键按需回查，不进内存也不进溢出文件。
//
// 结果：内存占用与结果集大小解耦，只与「可视窗口 + 预算」相关。
package rs

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"tacivan/internal/dbx"
)

// rowLoc 一行在溢出文件中的位置。
type rowLoc struct {
	off int64
	ln  int32
}

// spillFile 追加写、随机读的行溢出文件。
type spillFile struct {
	f    *os.File
	w    *bufio.Writer
	path string
	// off 下一次写入的偏移。
	off int64
	// dirty 表示缓冲区中有尚未落盘的数据。
	dirty bool
	// encBuf 复用的编码缓冲，避免每行一次分配。
	encBuf []byte
}

func newSpillFile(dir, id string) (*spillFile, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建溢出目录失败: %w", err)
	}
	path := filepath.Join(dir, "rs-"+id+".spill")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建溢出文件失败: %w", err)
	}
	return &spillFile{
		f:      f,
		w:      bufio.NewWriterSize(f, 256<<10),
		path:   path,
		encBuf: make([]byte, 0, 8<<10),
	}, nil
}

// append 写入一行，返回其位置。
func (s *spillFile) append(row dbx.Row) (rowLoc, error) {
	s.encBuf = encodeRow(s.encBuf[:0], row)
	n, err := s.w.Write(s.encBuf)
	if err != nil {
		return rowLoc{}, fmt.Errorf("写入溢出文件失败: %w", err)
	}
	loc := rowLoc{off: s.off, ln: int32(n)}
	s.off += int64(n)
	s.dirty = true
	return loc, nil
}

// readAt 读回一行。
func (s *spillFile) readAt(loc rowLoc, buf []byte) (dbx.Row, []byte, error) {
	if s.dirty {
		if err := s.w.Flush(); err != nil {
			return nil, buf, fmt.Errorf("刷新溢出文件失败: %w", err)
		}
		s.dirty = false
	}
	if cap(buf) < int(loc.ln) {
		buf = make([]byte, loc.ln)
	}
	buf = buf[:loc.ln]
	if _, err := s.f.ReadAt(buf, loc.off); err != nil {
		return nil, buf, fmt.Errorf("读取溢出文件失败: %w", err)
	}
	row, err := decodeRow(buf)
	return row, buf, err
}

func (s *spillFile) close() {
	if s.f != nil {
		_ = s.w.Flush()
		_ = s.f.Close()
		_ = os.Remove(s.path)
		s.f = nil
	}
}

// sizeOnDisk 当前溢出文件大小。
func (s *spillFile) sizeOnDisk() int64 { return s.off }

// --- 行编码 ---
//
// 紧凑二进制格式，比 gob 省一半空间且无反射开销：
//
//	varint  cellCount
//	每个 cell：
//	  byte    flags  (bit0=null, bit1=binary, bit2=truncated)
//	  varint  size   (原始字节长度)
//	  varint  textLen
//	  bytes   text
const (
	flagNull = 1 << iota
	flagBinary
	flagTruncated
)

func encodeRow(dst []byte, row dbx.Row) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(row)))
	for i := range row {
		c := &row[i]
		var flags byte
		if c.Null {
			flags |= flagNull
		}
		if c.Binary {
			flags |= flagBinary
		}
		if c.Truncated {
			flags |= flagTruncated
		}
		dst = append(dst, flags)
		dst = binary.AppendUvarint(dst, uint64(c.Size))
		dst = binary.AppendUvarint(dst, uint64(len(c.Text)))
		dst = append(dst, c.Text...)
	}
	return dst
}

func decodeRow(src []byte) (dbx.Row, error) {
	n, off := binary.Uvarint(src)
	if off <= 0 {
		return nil, fmt.Errorf("溢出记录损坏: 无法读取列数")
	}
	row := make(dbx.Row, n)
	for i := uint64(0); i < n; i++ {
		if off >= len(src) {
			return nil, fmt.Errorf("溢出记录损坏: 第 %d 列越界", i)
		}
		flags := src[off]
		off++

		size, k := binary.Uvarint(src[off:])
		if k <= 0 {
			return nil, fmt.Errorf("溢出记录损坏: 第 %d 列长度字段异常", i)
		}
		off += k

		textLen, k := binary.Uvarint(src[off:])
		if k <= 0 {
			return nil, fmt.Errorf("溢出记录损坏: 第 %d 列文本长度字段异常", i)
		}
		off += k

		if off+int(textLen) > len(src) {
			return nil, fmt.Errorf("溢出记录损坏: 第 %d 列文本越界", i)
		}
		row[i] = dbx.Cell{
			Null:      flags&flagNull != 0,
			Binary:    flags&flagBinary != 0,
			Truncated: flags&flagTruncated != 0,
			Size:      int(size),
			// string(...) 复制一份，确保不持有共享的读缓冲。
			Text: string(src[off : off+int(textLen)]),
		}
		off += int(textLen)
	}
	return row, nil
}
