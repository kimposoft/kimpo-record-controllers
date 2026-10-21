// Package router 实现数据动作聚合插件的分派层：把 extract / create / update / delete /
// detailsort 五个子包按动作名聚合为单一 bizpb.BusinessPluginServer，供单进程 sidecar 贡献
// 五个数据动作。「发起流程」不属于本插件（由流程引擎插件贡献），避免跨插件重复注册。
package router

import (
	"context"

	"github.com/kimposoft/kimpo-plugin-sdk/direction/biz"
	"github.com/kimposoft/kimpo-plugin-sdk/gen/bizpb"
	"github.com/kimposoft/kimpo-record-controllers/internal/actions/create"
	deleteaction "github.com/kimposoft/kimpo-record-controllers/internal/actions/delete"
	"github.com/kimposoft/kimpo-record-controllers/internal/actions/detailsort"
	"github.com/kimposoft/kimpo-record-controllers/internal/actions/extract"
	"github.com/kimposoft/kimpo-record-controllers/internal/actions/update"
)

// Service 路由聚合插件：把五个数据动作子包（extract/create/update/delete/detailsort）
// 合并为单进程贡献。GetActions 拼接五者动作定义；Execute/ValidateParams 按 req 动作名分派到对应子包。
type Service struct {
	bizpb.UnimplementedBusinessPluginServer
	extract *extract.Service
	create  *create.Service
	update  *update.Service
	del     *deleteaction.Service
	dsort   *detailsort.Service
	// byAction 用动作名索引到子 service，Execute/ValidateParams O(1) 分派。
	// key 直接取各子包导出的 ActionName 常量，避免与 GetActions 里声明的动作名发生字符串漂移。
	byAction map[string]bizpb.BusinessPluginServer
}

// New 构造聚合 service，装配五个子动作并建好分派表。
func New() *Service {
	ex, cr, up, de, dsort := extract.New(), create.New(), update.New(), deleteaction.New(), detailsort.New()
	svc := &Service{extract: ex, create: cr, update: up, del: de, dsort: dsort}
	svc.byAction = map[string]bizpb.BusinessPluginServer{
		extract.ActionName:      ex,
		create.ActionName:       cr,
		update.ActionName:       up,
		deleteaction.ActionName: de,
		detailsort.ActionName:   dsort,
	}
	return svc
}

// SetExtractionDetailWriteChunkSize 转发实例配置项 extraction_detail_write_chunk_size 给
// extract 子服务（cmd/plugin 读取实例配置后经本方法转发）。
func (s *Service) SetExtractionDetailWriteChunkSize(size int) {
	s.extract.SetDetailWriteChunkSize(size)
}

// GetActions 依次收集五个子包的动作定义并拼接返回；任一子调用出错直接透传。
func (s *Service) GetActions(ctx context.Context, req *bizpb.GetActionsRequest) (*bizpb.GetActionsResponse, error) {
	var actions []*bizpb.ActionDefinition

	exResp, err := s.extract.GetActions(ctx, req)
	if err != nil {
		return nil, err
	}
	actions = append(actions, exResp.GetActions()...)

	crResp, err := s.create.GetActions(ctx, req)
	if err != nil {
		return nil, err
	}
	actions = append(actions, crResp.GetActions()...)

	upResp, err := s.update.GetActions(ctx, req)
	if err != nil {
		return nil, err
	}
	actions = append(actions, upResp.GetActions()...)

	deResp, err := s.del.GetActions(ctx, req)
	if err != nil {
		return nil, err
	}
	actions = append(actions, deResp.GetActions()...)

	dsResp, err := s.dsort.GetActions(ctx, req)
	if err != nil {
		return nil, err
	}
	actions = append(actions, dsResp.GetActions()...)

	return &bizpb.GetActionsResponse{Actions: actions}, nil
}

// Execute 按 req.ActionName 分派到对应子包；未知动作名返回统一失败态，不 panic 不静默。
func (s *Service) Execute(ctx context.Context, req *bizpb.ExecuteRequest) (*bizpb.ExecuteResponse, error) {
	sub, ok := s.byAction[req.GetActionName()]
	if !ok {
		return biz.Fail("ACTION_UNKNOWN", "action.unknown", "未知动作: "+req.GetActionName()), nil
	}
	return sub.Execute(ctx, req)
}

// ValidateParams 按 req.ActionName 分派到对应子包；未知动作名返回 Valid:false + 一条校验错误说明。
func (s *Service) ValidateParams(ctx context.Context, req *bizpb.ValidateParamsRequest) (*bizpb.ValidateParamsResponse, error) {
	sub, ok := s.byAction[req.GetActionName()]
	if !ok {
		return &bizpb.ValidateParamsResponse{
			Valid: false,
			Errors: []*bizpb.ValidationError{
				{FieldPath: "action_name", Message: "未知动作: " + req.GetActionName()},
			},
		}, nil
	}
	return sub.ValidateParams(ctx, req)
}
