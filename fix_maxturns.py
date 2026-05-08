import os

filepath = "D:\\project\\go\\go-magic\\internal\\agent\\agent.go"
with open(filepath, "r", encoding="utf-8") as f:
    content = f.read()

content = content.replace("maxTurns:           100,", "maxTurns:           50,")

with open(filepath, "w", encoding="utf-8") as f:
    f.write(content)
print("Done! maxTurns set to 50")
