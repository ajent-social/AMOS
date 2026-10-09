package sqlstore

const actorCapacitySQL = `SELECT limit_bytes,used_bytes FROM amos_operation_actor_capacity WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND actor_kind=$4 AND actor_id=$5 FOR UPDATE`
const workspaceCapacitySQL = `SELECT limit_bytes,used_bytes FROM amos_operation_workspace_capacity WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND actor_kind=$4 AND actor_id=$5 AND workspace_id=$6 FOR UPDATE`
const updateActorSQL = `UPDATE amos_operation_actor_capacity SET used_bytes=$6 WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND actor_kind=$4 AND actor_id=$5`
const updateWorkspaceSQL = `UPDATE amos_operation_workspace_capacity SET used_bytes=$7 WHERE installation_id=$1 AND application_id=$2 AND environment_id=$3 AND actor_kind=$4 AND actor_id=$5 AND workspace_id=$6`
const selectInvocation = `SELECT id,installation_id,application_id,environment_id,actor_kind,actor_id,workspace_id,operation_id,key_digest,request_hash,operation_revision,descriptor_digest,input_schema_digest,output_schema_digest,reserved_bytes,state,result_kind,result_json,result_sha256,created_at,completed_at FROM amos_operation_invocations`
const insertInvocation = `INSERT INTO amos_operation_invocations (id,installation_id,application_id,environment_id,actor_kind,actor_id,workspace_id,operation_id,key_digest,request_hash,operation_revision,descriptor_digest,input_schema_digest,output_schema_digest,reserved_bytes,state) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'pending')`
const completeInvocation = `UPDATE amos_operation_invocations SET state='completed',result_kind=$2,result_json=$3,result_sha256=$4,reserved_bytes=$5 WHERE id=$1 AND state='pending'`
