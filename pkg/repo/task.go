package repo

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"pipeline/pkg/models"
)

func CreateTask(t *models.WorkflowTask) error {
	_, err := taskColl.InsertOne(context.TODO(), t)
	return err
}

// FindTask —— 按 (workflow_name, task_id) 取整棵任务树
// "整棵读"是这套数据的唯一读法（聚合根访问模式），从不跨实体 JOIN
func FindTask(workflowName string, taskID int64) (*models.WorkflowTask, error) {
	var t models.WorkflowTask
	err := taskColl.FindOne(context.TODO(), bson.M{
		"workflow_name": workflowName,
		"task_id":       taskID,
	}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTask —— ⭐ ack() 的落点（对照 Zadig mongodb/workflow_task_v4.go:318）
// 任务每前进一小步，就把【整棵树】一次性写回：ReplaceOne 单文档原子替换。
// 这是选 MongoDB 的核心理由之一：MySQL 下等价操作 = 一个事务 UPDATE 7-8 张表。
func UpdateTask(t *models.WorkflowTask) error {
	if t.ID == "" {
		return errors.New("update task: empty _id")
	}
	_, err := taskColl.ReplaceOne(context.TODO(), bson.M{"_id": t.ID}, t)
	return err
}

// InCompletedTasks —— M3.5 启动宣判的扫描源（对照 Zadig workflow_task_v4.go:259 同名函数）。
// 只捞 created/queued/running：这三种状态的看护人 goroutine 已随上一世进程死掉
// （queued 也没等到派发——T1.6 拍板：重启即排空，用户重跑）。
// ⚠️ 故意不含 pause——暂停是无需看护人的稳态（M3 灵魂），跨重启存活；
// Zadig 原版连 waiting-approve 一起取消，这里是有意的分叉点。
func InCompletedTasks() ([]*models.WorkflowTask, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cur, err := taskColl.Find(context.TODO(), bson.M{
		"status": bson.M{"$in": []models.Status{models.StatusCreated, models.StatusQueued, models.StatusRunning}},
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())

	var out []*models.WorkflowTask
	for cur.Next(context.TODO()) {
		var t models.WorkflowTask
		if err := cur.Decode(&t); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, cur.Err()
}

// ListQueued —— T1.6 派发器的扫描源：status=queued，task_id 升序（先来先服务）。
// 必须返回整棵树：抢占了就要直接喂给 engine.Run，投影省的那点流量换不来半棵树跑错。
func ListQueued() ([]*models.WorkflowTask, error) {
	opts := options.Find().SetSort(bson.D{{Key: "task_id", Value: 1}})
	cur, err := taskColl.Find(context.TODO(), bson.M{"status": models.StatusQueued}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.TODO())

	var out []*models.WorkflowTask
	for cur.Next(context.TODO()) {
		var t models.WorkflowTask
		if err := cur.Decode(&t); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, cur.Err()
}

// RunningStats —— 派发器的并发视图：全局 running 总数 + 按 workflow 分组（同 wf 串行的依据）。
// pause 不在场（它没有 goroutine，不占名额——T1.6 交互清单第一条）。
func RunningStats() (total int64, byWf map[string]int64, err error) {
	byWf = map[string]int64{}
	cur, err := taskColl.Find(context.TODO(), bson.M{"status": models.StatusRunning},
		options.Find().SetProjection(bson.M{"workflow_name": 1}))
	if err != nil {
		return 0, nil, err
	}
	defer cur.Close(context.TODO())
	for cur.Next(context.TODO()) {
		var row struct {
			WorkflowName string `bson:"workflow_name"`
		}
		if err := cur.Decode(&row); err != nil {
			return 0, nil, err
		}
		total++
		byWf[row.WorkflowName]++
	}
	return total, byWf, cur.Err()
}

// ClaimQueued —— T1.6 CAS 抢占：仍是 queued 才翻 running。派发器凭 MatchedCount==1
// 才起 goroutine——同一拍内/相邻拍间的双派、以及与 cancel 数据收口的竞态，都被
// 这一次条件更新挡在 Mongo 的单文档原子性上。
func ClaimQueued(t *models.WorkflowTask) (bool, error) {
	res, err := taskColl.UpdateOne(context.TODO(),
		bson.M{"_id": t.ID, "status": models.StatusQueued},
		bson.M{"$set": bson.M{"status": models.StatusRunning}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// TaskFilter —— T4.1 任务列表过滤/分页（对照 Zadig ListWorkflowTaskV4ByFilter 的极简版）
type TaskFilter struct {
	Status  string // 精确匹配（空 = 不过滤）
	Creator string // 精确匹配（空 = 不过滤）
	From    int64  // created_at ≥ From（unix 秒；0 = 不限）
	To      int64  // created_at ≤ To（unix 秒；0 = 不限）
	Page    int64  // 从 1 起；0 = 1
	Size    int64  // 默认 20；上限 100（防止一把梭全表）
}

func (f *TaskFilter) Normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Size < 1 {
		f.Size = 20
	}
	if f.Size > 100 {
		f.Size = 100
	}
}

// ListTasksFiltered —— 过滤 + 分页 + 总数（分页界面的三件套）。排序 task_id 降序（新的在前）。
// 返回归一化后的 filter（Page/Size 已填默认值）——调用方直接用它组响应，不用再猜边界。
func ListTasksFiltered(workflowName string, f TaskFilter) ([]*models.WorkflowTask, int64, TaskFilter, error) {
	f.Normalize()
	q := bson.M{"workflow_name": workflowName}
	if f.Status != "" {
		q["status"] = models.Status(f.Status)
	}
	if f.Creator != "" {
		q["creator"] = f.Creator
	}
	if f.From > 0 || f.To > 0 {
		rng := bson.M{}
		if f.From > 0 {
			rng["$gte"] = time.Unix(f.From, 0)
		}
		if f.To > 0 {
			rng["$lte"] = time.Unix(f.To, 0)
		}
		q["created_at"] = rng
	}

	total, err := taskColl.CountDocuments(context.TODO(), q)
	if err != nil {
		return nil, 0, f, err
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "task_id", Value: -1}}).
		SetSkip((f.Page - 1) * f.Size).
		SetLimit(f.Size).
		SetProjection(bson.M{"stages": 0, "workflow_args": 0}) // 列表页不拉整棵树
	cur, err := taskColl.Find(context.TODO(), q, opts)
	if err != nil {
		return nil, 0, f, err
	}
	defer cur.Close(context.TODO())

	var out []*models.WorkflowTask
	for cur.Next(context.TODO()) {
		var t models.WorkflowTask
		if err := cur.Decode(&t); err != nil {
			return nil, 0, f, err
		}
		out = append(out, &t)
	}
	return out, total, f, cur.Err()
}
