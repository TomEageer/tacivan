// Package sshtun 提供 SSH 隧道，让各引擎驱动可以透过跳板机连库。
package sshtun

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"tacivan/internal/dbx"
)

var netSeq atomic.Uint64

// Tunnel 一条已建立的 SSH 连接，用作数据库连接的拨号器。
type Tunnel struct {
	client  *ssh.Client
	netName string
	target  string

	mu     sync.Mutex
	closed bool
}

// Open 建立 SSH 隧道。target 是隧道另一端要连的地址（host:port）。
func Open(cfg dbx.SSHConfig, target string) (*Tunnel, error) {
	auth, err := buildAuth(cfg)
	if err != nil {
		return nil, err
	}
	hostKeyCallback, err := hostKeyChecker()
	if err != nil {
		return nil, err
	}

	port := cfg.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("SSH 连接 %s 失败: %w", addr, err)
	}

	t := &Tunnel{
		client:  client,
		netName: fmt.Sprintf("navigo-ssh-%d", netSeq.Add(1)),
		target:  target,
	}
	// go-sql-driver 通过网络名查找自定义拨号器。
	gomysql.RegisterDialContext(t.netName, func(ctx context.Context, addr string) (net.Conn, error) {
		return t.DialContext(ctx, "tcp", addr)
	})
	return t, nil
}

// NetName 供 MySQL DSN 使用的网络名。
func (t *Tunnel) NetName() string { return t.netName }

// Target 隧道另一端的地址。
func (t *Tunnel) Target() string { return t.target }

// DialContext 透过 SSH 连接目标地址。
func (t *Tunnel) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	t.mu.Lock()
	closed := t.closed
	client := t.client
	t.mu.Unlock()
	if closed || client == nil {
		return nil, fmt.Errorf("SSH 隧道已关闭")
	}
	if addr == "" {
		addr = t.target
	}
	// x/crypto/ssh 的 Dial 不接受 context，用 goroutine + select 兑现取消语义。
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := client.Dial(network, addr)
		ch <- result{c, err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.conn != nil {
				r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("通过 SSH 隧道连接 %s 失败: %w", addr, r.err)
		}
		return r.conn, nil
	}
}

// Close 关闭隧道。
func (t *Tunnel) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	if t.client != nil {
		_ = t.client.Close()
		t.client = nil
	}
	gomysql.DeregisterDialContext(t.netName)
}

func buildAuth(cfg dbx.SSHConfig) ([]ssh.AuthMethod, error) {
	switch cfg.AuthMethod {
	case "password":
		if cfg.Password == "" {
			return nil, fmt.Errorf("SSH 密码为空")
		}
		return []ssh.AuthMethod{ssh.Password(cfg.Password)}, nil

	case "agent":
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil, fmt.Errorf("未找到 ssh-agent（SSH_AUTH_SOCK 未设置）")
		}
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, fmt.Errorf("连接 ssh-agent 失败: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeysCallback(agent.NewClient(conn).Signers)}, nil

	default: // key
		path := cfg.KeyFile
		if path == "" {
			path = filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa")
		}
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取私钥失败: %w", err)
		}
		var signer ssh.Signer
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(pem)
		}
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败（如已加密请填写口令）: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
}

// hostKeyChecker 基于 ~/.ssh/known_hosts 校验主机密钥。
//
// 刻意不提供「跳过校验」选项：连数据库通常意味着跳板机后面就是生产环境，
// 关掉主机校验等于把中间人攻击的门敞开。未知主机时给出明确的补救指引。
func hostKeyChecker() (ssh.HostKeyCallback, error) {
	path := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("找不到 %s，请先用 ssh 命令手动连接一次该主机以记录主机密钥", path)
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("解析 known_hosts 失败: %w", err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := cb(hostname, remote, key); err != nil {
			return fmt.Errorf("主机密钥校验失败（%s）：若为首次连接，请先执行 ssh %s 确认指纹: %w",
				hostname, hostname, err)
		}
		return nil
	}, nil
}
