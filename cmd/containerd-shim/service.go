package main

import (
	"fmt"
	"sync"

	runc "github.com/crosbymichael/go-runc"
	"github.com/docker/containerd/api/shim"
	"github.com/docker/containerd/utils"
	google_protobuf "github.com/golang/protobuf/ptypes/empty"
	"golang.org/x/net/context"
)

var emptyResponse = &google_protobuf.Empty{}

type service struct {
	initPid   int
	mu        sync.Mutex
	processes map[int]process
}

func (s *service) Create(ctx context.Context, r *shim.CreateRequest) (*shim.CreateResponse, error) {
	process, err := newInitProcess(ctx, r)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	pid := process.Pid()
	s.initPid, s.processes[pid] = pid, process
	s.mu.Unlock()
	return &shim.CreateResponse{
		Pid: uint32(pid),
	}, nil
}

func (s *service) Start(ctx context.Context, r *shim.StartRequest) (*google_protobuf.Empty, error) {
	s.mu.Lock()
	p := s.processes[s.initPid]
	s.mu.Unlock()
	if err := p.Start(ctx); err != nil {
		return nil, err
	}
	return emptyResponse, nil
}

func (s *service) Delete(ctx context.Context, r *shim.DeleteRequest) (*shim.DeleteResponse, error) {
	s.mu.Lock()
	p, ok := s.processes[int(r.Pid)]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("process does not exist %d", r.Pid)
	}
	if err := p.Delete(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	delete(s.processes, int(r.Pid))
	s.mu.Unlock()
	return &shim.DeleteResponse{
		ExitStatus: uint32(p.Status()),
	}, nil
}

func (s *service) Exec(ctx context.Context, r *shim.ExecRequest) (*shim.ExecResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	process, err := newExecProcess(ctx, r, s.processes[s.initPid].(*initProcess))
	if err != nil {
		return nil, err
	}
	pid := process.Pid()
	s.processes[pid] = process
	return &shim.ExecResponse{
		Pid: uint32(pid),
	}, nil
}

func (s *service) Pty(ctx context.Context, r *shim.PtyRequest) (*google_protobuf.Empty, error) {
	ws := runc.WinSize{
		Width:  uint16(r.Width),
		Height: uint16(r.Height),
	}
	s.mu.Lock()
	p, ok := s.processes[int(r.Pid)]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("process does not exist %d", r.Pid)
	}
	if err := p.Resize(ws); err != nil {
		return nil, err
	}
	return emptyResponse, nil
}

func (s *service) processExited(e utils.Exit) error {
	s.mu.Lock()
	if p, ok := s.processes[e.Pid]; ok {
		p.Exited(e.Status)
	}
	s.mu.Unlock()
	return nil
}
