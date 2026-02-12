-- name: Insert :one
INSERT INTO user_settings (
    chatid, name, settings, step, status,premium
) VALUES (
             @chatid, @name, @settings, @step::text, @status, @premium
         ) returning id;


-- name: Update :exec
UPDATE user_settings
SET  name = @name, settings = @settings, step = @step::text, status = @status,premium = @premium
WHERE chatid = @chatid;

-- name: ListEnabled :many
SELECT *
FROM user_settings
WHERE status = true;




-- name: Delete :exec
DELETE FROM user_settings
WHERE chatid = @chatid and  id = @id;


-- name: GetById :one
SELECT id, name, settings, step::text,status,premium FROM user_settings WHERE chatid = @chatid;


-- name: GetAll :many
SELECT id, chatid, name, settings, step::text, status,premium FROM user_settings;