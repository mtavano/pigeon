package schedulersvc

import (
	"golang.org/x/net/context"

	"github.com/iampigeon/pigeon"
	pb "github.com/iampigeon/pigeon/proto"
	"github.com/iampigeon/pigeon/scheduler"
	"github.com/oklog/ulid"
)

var _ pb.SchedulerServiceServer = (*Service)(nil)

// Service ...
type Service struct {
	schedulerSvc pigeon.SchedulerService
}

// New ...
func New(config scheduler.StorageConfig) *Service {
	return &Service{
		schedulerSvc: scheduler.New(config),
	}
}

// Put ...
func (s *Service) Put(ctx context.Context, r *pb.PutRequest) (*pb.PutResponse, error) {
	id, err := ulid.Parse(r.Id)
	if err != nil {
		return nil, err
	}

	if err := s.schedulerSvc.Put(id, r.Content, pigeon.NetAddr(r.Endpoint), pigeon.StatusPending, r.SubjectId, r.UserId); err != nil {
		return nil, err
	}

	return &pb.PutResponse{}, nil
}
func (s *Service) Get(ctx context.Context, r *pb.GetRequest) (*pb.GetResponse, error) {
	id, err := ulid.Parse(r.Id)
	if err != nil {
		return nil, err
	}

	u := &pigeon.User{ID: r.UserId}
	msg, err := s.schedulerSvc.Get(id, u)
	if err != nil {
		return nil, err
	}

	return &pb.GetResponse{
		Message: &pb.Message{
			Id:        r.Id,
			Content:   msg.Content,
			Endpoint:  string(msg.Endpoint),
			Status:    string(msg.Status),
			SubjectId: string(msg.SubjectID),
		},
	}, nil
}
func (s *Service) Update(ctx context.Context, r *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	id, err := ulid.Parse(r.Id)
	if err != nil {
		return nil, err
	}

	if err := s.schedulerSvc.Update(id, r.Content); err != nil {
		return nil, err
	}

	return &pb.UpdateResponse{}, nil
}

// Cancel ...
func (s *Service) Cancel(ctx context.Context, r *pb.CancelRequest) (*pb.CancelResponse, error) {
	id, err := ulid.Parse(r.Id)
	if err != nil {
		return nil, err
	}

	if err := s.schedulerSvc.Cancel(id); err != nil {
		return nil, err
	}
	return &pb.CancelResponse{}, nil
}
