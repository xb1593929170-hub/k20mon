-- ---------------------------------------------------------------------------
-- 建一个管理员账号
--
-- 本文件只含 SQL 模板，不含任何真实账号或密码。
--
-- 直接执行本文件是不行的：password 必须是 bcrypt hash，不能写明文。
-- 两种用法：
--
--   1) 用脚本自动生成 hash 并插入（推荐）：
--        ./deploy/sql/create-admin.sh --host 127.0.0.1 --user root --password MySQL密码 \
--            --db k20mon --username admin --app-pass '你的登录密码'
--
--   2) 自己在 MySQL 客户端里手工执行：
--        先算 hash：
--          python3 -c "import bcrypt;print(bcrypt.hashpw(b'你的登录密码', bcrypt.gensalt()).decode())"
--        再把下面的 <BCRYPT_HASH> 换成结果，然后执行。
--
-- 注意 hash 里含 `$` 字符，shell 里传参必须用单引号。
-- ---------------------------------------------------------------------------

USE `k20mon`;

INSERT INTO `sys_users` (`uuid`, `username`, `password`, `nick_name`, `authority_id`, `enable`)
VALUES (
  UUID(),
  'admin',
  '<BCRYPT_HASH>',
  '管理员',
  888,
  1
)
ON DUPLICATE KEY UPDATE
  `password`   = VALUES(`password`),
  `nick_name`  = VALUES(`nick_name`),
  `enable`     = 1;
