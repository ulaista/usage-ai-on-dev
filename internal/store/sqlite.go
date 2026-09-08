package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ulaista/usage-ai-on-dev/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store,error){
	if err:=os.MkdirAll(filepath.Dir(path),0o755);err!=nil{return nil,err}
	db,err:=sql.Open("sqlite","file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)");if err!=nil{return nil,err}
	db.SetMaxOpenConns(8);if err:=db.Ping();err!=nil{db.Close();return nil,err}
	s:=&Store{db:db};if err:=s.migrate(context.Background());err!=nil{db.Close();return nil,err};return s,nil
}
func (s *Store) Close() error{return s.db.Close()}

func (s *Store) migrate(ctx context.Context) error{
	statements:=[]string{
		`CREATE TABLE IF NOT EXISTS intents (id TEXT PRIMARY KEY,title TEXT NOT NULL,goal TEXT NOT NULL,status TEXT NOT NULL,constraints_json TEXT NOT NULL DEFAULT '[]',affected_json TEXT NOT NULL DEFAULT '[]',verification_json TEXT NOT NULL DEFAULT '[]',created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS batches (id TEXT PRIMARY KEY,intent_id TEXT NOT NULL,title TEXT NOT NULL,scope_json TEXT NOT NULL DEFAULT '[]',status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,FOREIGN KEY(intent_id) REFERENCES intents(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS executions (id TEXT PRIMARY KEY,task TEXT NOT NULL,task_type TEXT NOT NULL,model TEXT NOT NULL,route TEXT NOT NULL,latency_ms INTEGER NOT NULL DEFAULT 0,input_tokens INTEGER NOT NULL DEFAULT 0,output_tokens INTEGER NOT NULL DEFAULT 0,accepted INTEGER NULL,fallback INTEGER NOT NULL DEFAULT 0,error TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS cache_savings (id INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,cache_key TEXT NOT NULL,saved_input_tokens INTEGER NOT NULL DEFAULT 0,saved_output_tokens INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_intents_status ON intents(status)`,`CREATE INDEX IF NOT EXISTS idx_batches_intent ON batches(intent_id)`,`CREATE INDEX IF NOT EXISTS idx_executions_model ON executions(model,created_at)`,`CREATE INDEX IF NOT EXISTS idx_cache_savings_kind ON cache_savings(kind,created_at)`,
	}
	for _,statement:=range statements{if _,err:=s.db.ExecContext(ctx,statement);err!=nil{return err}};return nil
}
func jsonText(v any)string{data,_:=json.Marshal(v);return string(data)}
func decodeList(raw string)[]string{var out []string;_ = json.Unmarshal([]byte(raw),&out);return out}
func (s *Store) CreateIntent(ctx context.Context,in domain.Intent)error{_,err:=s.db.ExecContext(ctx,`INSERT INTO intents (id,title,goal,status,constraints_json,affected_json,verification_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,in.ID,in.Title,in.Goal,in.Status,jsonText(in.Constraints),jsonText(in.Affected),jsonText(in.Verification),in.CreatedAt.UTC().Format(time.RFC3339Nano),in.UpdatedAt.UTC().Format(time.RFC3339Nano));return err}
func (s *Store) ListActiveIntents(ctx context.Context)([]domain.Intent,error){rows,err:=s.db.QueryContext(ctx,`SELECT id,title,goal,status,constraints_json,affected_json,verification_json,created_at,updated_at FROM intents WHERE status='active' ORDER BY created_at DESC`);if err!=nil{return nil,err};defer rows.Close();var result []domain.Intent;for rows.Next(){var in domain.Intent;var constraints,affected,verification,created,updated string;if err:=rows.Scan(&in.ID,&in.Title,&in.Goal,&in.Status,&constraints,&affected,&verification,&created,&updated);err!=nil{return nil,err};in.Constraints=decodeList(constraints);in.Affected=decodeList(affected);in.Verification=decodeList(verification);in.CreatedAt,_=time.Parse(time.RFC3339Nano,created);in.UpdatedAt,_=time.Parse(time.RFC3339Nano,updated);result=append(result,in)};return result,rows.Err()}
func (s *Store) CreateBatch(ctx context.Context,b domain.Batch)error{_,err:=s.db.ExecContext(ctx,`INSERT INTO batches(id,intent_id,title,scope_json,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,b.ID,b.IntentID,b.Title,jsonText(b.Scope),b.Status,b.CreatedAt.UTC().Format(time.RFC3339Nano),b.UpdatedAt.UTC().Format(time.RFC3339Nano));return err}
func (s *Store) RecordExecution(ctx context.Context,e domain.Execution)error{var accepted any;if e.Accepted!=nil{if *e.Accepted{accepted=1}else{accepted=0}};_,err:=s.db.ExecContext(ctx,`INSERT OR REPLACE INTO executions (id,task,task_type,model,route,latency_ms,input_tokens,output_tokens,accepted,fallback,error,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,e.ID,e.Task,e.TaskType,e.Model,e.Route,e.LatencyMillis,e.InputTokens,e.OutputTokens,accepted,boolInt(e.Fallback),e.Error,e.CreatedAt.UTC().Format(time.RFC3339Nano));return err}
func (s *Store) MarkExecution(ctx context.Context,id string,accepted bool)error{res,err:=s.db.ExecContext(ctx,`UPDATE executions SET accepted=? WHERE id=?`,boolInt(accepted),id);if err!=nil{return err};n,err:=res.RowsAffected();if err!=nil{return err};if n==0{return fmt.Errorf("execution %s not found",id)};return nil}
func (s *Store) TelemetrySummary(ctx context.Context,model string)(domain.TelemetrySummary,error){where:="";args:=[]any{};if model!=""{where=" WHERE model=?";args=append(args,model)};query:=`SELECT COUNT(*),COALESCE(SUM(CASE WHEN accepted IS NOT NULL THEN 1 ELSE 0 END),0),COALESCE(AVG(CASE WHEN accepted IS NOT NULL THEN accepted END),0),COALESCE(AVG(latency_ms),0),COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),COALESCE(SUM(fallback),0),COALESCE(SUM(CASE WHEN error<>'' THEN 1 ELSE 0 END),0) FROM executions`+where;var out domain.TelemetrySummary;err:=s.db.QueryRowContext(ctx,query,args...).Scan(&out.Samples,&out.Reviewed,&out.AcceptanceRate,&out.AverageLatencyMS,&out.InputTokens,&out.OutputTokens,&out.Fallbacks,&out.Errors);return out,err}
func (s *Store) RecordSaving(ctx context,saving domain.CacheSaving)error{_,err:=s.db.ExecContext(ctx,`INSERT INTO cache_savings(kind,cache_key,saved_input_tokens,saved_output_tokens,created_at) VALUES(?,?,?,?,?)`,saving.Kind,saving.Key,saving.SavedInputTokens,saving.SavedOutputTokens,saving.CreatedAt.UTC().Format(time.RFC3339Nano));return err}
func (s *Store) EconomySummary(ctx context.Context)(domain.EconomySummary,error){var out domain.EconomySummary;err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*),COALESCE(SUM(CASE WHEN kind='context' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN kind='execution' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN kind='autotune' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN kind='singleflight' THEN 1 ELSE 0 END),0),COALESCE(SUM(saved_input_tokens),0),COALESCE(SUM(saved_output_tokens),0) FROM cache_savings`).Scan(&out.Hits,&out.ContextHits,&out.ExecutionHits,&out.AutotuneHits,&out.SingleflightJoins,&out.SavedInputTokens,&out.SavedOutputTokens);out.SavedTotalTokens=out.SavedInputTokens+out.SavedOutputTokens;return out,err}
func boolInt(v bool)int{if v{return 1};return 0}
