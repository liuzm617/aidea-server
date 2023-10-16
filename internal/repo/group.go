package repo

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mylxsw/aidea-server/internal/repo/model"
	"github.com/mylxsw/eloquent"
	"github.com/mylxsw/eloquent/query"
	"github.com/mylxsw/go-utils/array"
	"gopkg.in/guregu/null.v3"
)

type ChatGroupRepo struct {
	db *sql.DB
}

func NewChatGroupRepo(db *sql.DB) *ChatGroupRepo {
	return &ChatGroupRepo{db: db}
}

type Member struct {
	ID        int    `json:"id,omitempty"`
	ModelID   string `json:"model_id"`
	ModelName string `json:"model_name,omitempty"`
}

const (
	// ChatGroupMemberStatusNormal 组成员状态：正常
	ChatGroupMemberStatusNormal = 1
	// ChatGroupMemberStatusDeleted 组成员状态：已删除
	ChatGroupMemberStatusDeleted = 2

	// ChatGroupMessageStatusWaiting 消息状态：待处理
	ChatGroupMessageStatusWaiting = 0
	// ChatGroupMessageStatusSucceed 消息状态：成功
	ChatGroupMessageStatusSucceed = 1
	// ChatGroupMessageStatusFailed 消息状态：失败
	ChatGroupMessageStatusFailed = 2
)

// CreateGroup 创建一个聊天群组
func (repo *ChatGroupRepo) CreateGroup(ctx context.Context, userID int64, name string, members []Member) (int64, error) {
	var groupID int64
	err := eloquent.Transaction(repo.db, func(tx query.Database) error {
		gid, err := model.NewChatGroupModel(tx).Create(ctx, query.KV{
			model.FieldChatGroupUserId: userID,
			model.FieldChatGroupName:   name,
		})
		if err != nil {
			return fmt.Errorf("create group failed: %w", err)
		}

		groupID = gid

		for _, member := range members {
			if _, err := model.NewChatGroupMemberModel(tx).Create(ctx, query.KV{
				model.FieldChatGroupMemberGroupId:   gid,
				model.FieldChatGroupMemberModelId:   member.ModelID,
				model.FieldChatGroupMemberModelName: member.ModelName,
				model.FieldChatGroupMemberStatus:    ChatGroupMemberStatusNormal,
			}); err != nil {
				return fmt.Errorf("create group member failed: %w", err)
			}
		}

		return nil
	})

	return groupID, err
}

// UpdateGroup 更新群组信息
func (repo *ChatGroupRepo) UpdateGroup(ctx context.Context, groupID int64, userID int64, name string) error {
	return eloquent.Transaction(repo.db, func(tx query.Database) error {
		q := query.Builder().Where(model.FieldChatGroupId, groupID).Where(model.FieldChatGroupUserId, userID)
		grp, err := model.NewChatGroupModel(tx).First(ctx, q)
		if err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}

			return fmt.Errorf("query group failed: %w", err)
		}

		if name != grp.Name.ValueOrZero() {
			grp.Name = null.StringFrom(name)
			if err := grp.Save(ctx); err != nil {
				return fmt.Errorf("save group failed: %w", err)
			}
		}

		return nil
	})
}

// UpdateGroupMembers 更新群组成员
func (repo *ChatGroupRepo) UpdateGroupMembers(ctx context.Context, groupID int64, userID int64, members []Member) error {
	return eloquent.Transaction(repo.db, func(tx query.Database) error {
		q := query.Builder().Where(model.FieldChatGroupMemberGroupId, groupID).
			Where(model.FieldChatGroupMemberStatus, ChatGroupMemberStatusNormal).
			Where(model.FieldChatGroupMemberUserId, userID)
		currentMembers, err := model.NewChatGroupMemberModel(tx).Get(ctx, q)
		if err != nil {
			return fmt.Errorf("query group members failed: %w", err)
		}

		membersMap := array.ToMap(members, func(member Member, _ int) int64 { return int64(member.ID) })
		currentMembersMap := array.ToMap(currentMembers, func(member model.ChatGroupMemberN, _ int) int64 { return member.Id.ValueOrZero() })

		for i, member := range currentMembers {
			if modifyMember, ok := membersMap[member.Id.ValueOrZero()]; !ok {
				// 1. 删除已经不存在的成员
				currentMembers[i].Status = null.IntFrom(ChatGroupMemberStatusDeleted)
			} else {
				// 2. 更新已经存在的成员
				member.ModelId = null.StringFrom(modifyMember.ModelID)
				member.ModelName = null.StringFrom(modifyMember.ModelName)
				currentMembers[i] = member
			}
		}

		// 3. 添加新成员
		for _, member := range members {
			if _, ok := currentMembersMap[int64(member.ID)]; !ok {
				currentMembers = append(currentMembers, model.ChatGroupMemberN{
					GroupId:   null.IntFrom(groupID),
					UserId:    null.IntFrom(userID),
					ModelId:   null.StringFrom(member.ModelID),
					ModelName: null.StringFrom(member.ModelName),
					Status:    null.IntFrom(ChatGroupMemberStatusNormal),
				})
			}
		}

		// 4. 保存
		for _, member := range currentMembers {
			if err := member.Save(ctx); err != nil {
				return fmt.Errorf("save group member failed: %w", err)
			}
		}

		return nil
	})
}

// AddMembersToGroup 添加成员到群组
func (repo *ChatGroupRepo) AddMembersToGroup(ctx context.Context, groupID, userID int64, members []Member) error {
	return eloquent.Transaction(repo.db, func(tx query.Database) error {
		for _, member := range members {
			if _, err := model.NewChatGroupMemberModel(tx).Create(ctx, query.KV{
				model.FieldChatGroupMemberGroupId:   groupID,
				model.FieldChatGroupMemberModelId:   member.ModelID,
				model.FieldChatGroupMemberModelName: member.ModelName,
				model.FieldChatGroupMemberStatus:    ChatGroupMemberStatusNormal,
			}); err != nil {
				return fmt.Errorf("create group member failed: %w", err)
			}
		}

		return nil
	})
}

// RemoveMembersFromGroup 从群组中移除成员
func (repo *ChatGroupRepo) RemoveMembersFromGroup(ctx context.Context, groupID, userID int64, memberIDs []int64) error {
	if len(memberIDs) == 0 {
		return nil
	}

	return eloquent.Transaction(repo.db, func(tx query.Database) error {
		q := query.Builder().Where(model.FieldChatGroupMemberGroupId, groupID).
			Where(model.FieldChatGroupMemberUserId, userID).
			Where(model.FieldChatGroupMemberStatus, ChatGroupMemberStatusNormal).
			WhereIn(model.FieldChatGroupMemberId, memberIDs)

		_, err := model.NewChatGroupMemberModel(tx).UpdateFields(ctx, query.KV{model.FieldChatGroupMemberStatus: ChatGroupMemberStatusDeleted}, q)
		return err
	})
}

type Group struct {
	Group   model.ChatGroup         `json:"group"`
	Members []model.ChatGroupMember `json:"members"`
}

// GetGroup 获取群组信息
func (repo *ChatGroupRepo) GetGroup(ctx context.Context, groupID int64, userID int64) (*Group, error) {
	// 1. 获取群组信息
	grp, err := model.NewChatGroupModel(repo.db).First(ctx, query.Builder().
		Where(model.FieldChatGroupId, groupID).
		Where(model.FieldChatGroupUserId, userID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf("query group failed: %w", err)
	}

	// 2. 获取群组成员信息
	members, err := model.NewChatGroupMemberModel(repo.db).Get(ctx, query.Builder().
		Where(model.FieldChatGroupMemberGroupId, groupID).
		Where(model.FieldChatGroupMemberStatus, ChatGroupMemberStatusNormal))
	if err != nil {
		return nil, fmt.Errorf("query group members failed: %w", err)
	}

	return &Group{
		Group: grp.ToChatGroup(),
		Members: array.Map(members, func(member model.ChatGroupMemberN, _ int) model.ChatGroupMember {
			return member.ToChatGroupMember()
		}),
	}, nil
}

// Groups 获取用户的群组列表
func (repo *ChatGroupRepo) Groups(ctx context.Context, userID int64, limit int64) ([]model.ChatGroup, error) {
	groups, err := model.NewChatGroupModel(repo.db).Get(ctx, query.Builder().
		Where(model.FieldChatGroupUserId, userID).
		OrderBy(model.FieldChatGroupId, "DESC").
		Limit(limit))
	if err != nil {
		return nil, fmt.Errorf("query groups failed: %w", err)
	}

	return array.Map(groups, func(group model.ChatGroupN, _ int) model.ChatGroup {
		return group.ToChatGroup()
	}), nil
}

type ChatGroupMessage struct {
	Message       string `json:"message,omitempty"`
	Role          int64  `json:"role,omitempty"`
	TokenConsumed int64  `json:"token_consumed,omitempty"`
	QuotaConsumed int64  `json:"quota_consumed,omitempty"`
	Pid           int64  `json:"pid,omitempty"`
	MemberId      int64  `json:"member_id,omitempty"`
	Status        int64  `json:"status,omitempty"`
}

// AddChatMessage 添加聊天消息
func (repo *ChatGroupRepo) AddChatMessage(ctx context.Context, groupID, userID int64, msg ChatGroupMessage) (int64, error) {
	var messageID int64
	err := eloquent.Transaction(repo.db, func(tx query.Database) error {

		chatMsg := model.ChatGroupMessage{
			GroupId:       groupID,
			UserId:        userID,
			Message:       msg.Message,
			Role:          msg.Role,
			TokenConsumed: msg.TokenConsumed,
			QuotaConsumed: msg.QuotaConsumed,
			Pid:           msg.Pid,
			MemberId:      msg.MemberId,
			Status:        msg.Status,
		}

		msgID, err := model.NewChatGroupMessageModel(tx).Save(ctx, chatMsg.ToChatGroupMessageN(
			model.FieldChatGroupMessageGroupId,
			model.FieldChatGroupMessageUserId,
			model.FieldChatGroupMessageMessage,
			model.FieldChatGroupMessageRole,
			model.FieldChatGroupMessageTokenConsumed,
			model.FieldChatGroupMessageQuotaConsumed,
			model.FieldChatGroupMessagePid,
			model.FieldChatGroupMessageMemberId,
			model.FieldChatGroupMessageStatus,
		))
		if err != nil {
			return fmt.Errorf("save chat message failed: %w", err)
		}

		messageID = msgID

		return nil
	})

	return messageID, err
}

// GetChatMessage 获取聊天消息
func (repo *ChatGroupRepo) GetChatMessage(ctx context.Context, groupID, userID, messageID int64) (*model.ChatGroupMessage, error) {
	q := query.Builder().
		Where(model.FieldChatGroupMessageGroupId, groupID).
		Where(model.FieldChatGroupMessageUserId, userID).
		Where(model.FieldChatGroupMessageId, messageID)
	msg, err := model.NewChatGroupMessageModel(repo.db).First(ctx, q)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf("query chat message failed: %w", err)

	}

	ret := msg.ToChatGroupMessage()
	return &ret, err
}

// GetChatMessages 获取聊天消息列表
func (repo *ChatGroupRepo) GetChatMessages(ctx context.Context, groupID int64, limit int64) ([]model.ChatGroupMessage, error) {
	messages, err := model.NewChatGroupMessageModel(repo.db).Get(ctx, query.Builder().
		Where(model.FieldChatGroupMessageGroupId, groupID).
		OrderBy(model.FieldChatGroupMessageId, "DESC").
		Limit(limit))
	if err != nil {
		return nil, fmt.Errorf("query chat messages failed: %w", err)
	}

	return array.Map(messages, func(message model.ChatGroupMessageN, _ int) model.ChatGroupMessage {
		return message.ToChatGroupMessage()
	}), nil
}

// DeleteChatMessage 删除聊天消息
func (repo *ChatGroupRepo) DeleteChatMessage(ctx context.Context, groupID, userID, messageID int64) error {
	return eloquent.Transaction(repo.db, func(tx query.Database) error {
		q := query.Builder().
			Where(model.FieldChatGroupMessageGroupId, groupID).
			Where(model.FieldChatGroupMessageUserId, userID).
			Where(model.FieldChatGroupMessageId, messageID)

		_, err := model.NewChatGroupMessageModel(tx).Delete(ctx, q)
		return err
	})
}

type ChatGroupMessageUpdate struct {
	Message       string `json:"message,omitempty"`
	TokenConsumed int64  `json:"token_consumed,omitempty"`
	QuotaConsumed int64  `json:"quota_consumed,omitempty"`
	Status        int64  `json:"status,omitempty"`
}

// UpdateChatMessage 更新聊天消息
func (repo *ChatGroupRepo) UpdateChatMessage(ctx context.Context, groupID, userID, messageID int64, msg ChatGroupMessageUpdate) error {
	return eloquent.Transaction(repo.db, func(tx query.Database) error {
		q := query.Builder().
			Where(model.FieldChatGroupMessageGroupId, groupID).
			Where(model.FieldChatGroupMessageUserId, userID).
			Where(model.FieldChatGroupMessageId, messageID)

		_, err := model.NewChatGroupMessageModel(tx).UpdateFields(ctx, query.KV{
			model.FieldChatGroupMessageMessage:       msg.Message,
			model.FieldChatGroupMessageTokenConsumed: msg.TokenConsumed,
			model.FieldChatGroupMessageQuotaConsumed: msg.QuotaConsumed,
			model.FieldChatGroupMessageStatus:        msg.Status,
		}, q)

		return err
	})
}