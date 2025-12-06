-- name: Insert :one
INSERT INTO user_settings (
    chatid, name, settings, step
) VALUES (
             @chatid, @name, @settings, @step::text
         ) returning id;


-- name: Update :exec
UPDATE user_settings
SET  name = @name, settings = @settings, step = @step::text
WHERE chatid = @chatid;



-- name: Delete :exec
DELETE FROM user_settings
WHERE chatid = @chatid and  id = @id;


-- name: GetById :one
SELECT id, name, settings, step::text FROM user_settings WHERE chatid = @chatid;


-- name: GetAll :many
SELECT id, chatid, name, settings, step::text FROM user_settings;