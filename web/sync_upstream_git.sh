set -x
# 添加上游（仅首次）
git remote add upstream git@github.com:yeying-community/yeying-assets.git

# 获取上游更新
git fetch upstream

# 切换到主分支并更新
git checkout main
git rebase upstream/main

# 推送到你的 fork
git push origin main
set +x