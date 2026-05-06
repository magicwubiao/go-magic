---
name: file-organizer
description: "Organize and structure files following best practices"
version: 1.0.0
author: go-magic
tags: [organization, files, structure, management]
tools: [list_files, directory_tree, file_edit, file_search]
---

# File Organizer Skill

## When to Use

Load this skill when:
- The user wants to organize their project files
- Setting up a new project structure
- Tidying up messy directories
- Creating consistent file layouts

## Project Structure Best Practices

### General Project Layout
```
project/
├── README.md
├── LICENSE
├── .gitignore
├── src/              # Source code
├── tests/            # Test files
├── docs/             # Documentation
├── config/           # Configuration files
├── scripts/          # Build/utility scripts
└── bin/              # Compiled binaries
```

### Web Project
```
web-project/
├── public/           # Static assets
├── src/
│   ├── components/   # UI components
│   ├── pages/        # Page components
│   ├── hooks/        # Custom hooks
│   └── utils/        # Utilities
└── package.json
```

### Python Project
```
python-project/
├── setup.py
├── requirements.txt
├── package/
│   ├── __init__.py
│   └── module.py
└── tests/
```

## Naming Conventions

| Type | Convention | Example |
|------|------------|---------|
| Files | kebab-case | `my-file.py` |
| Classes | PascalCase | `MyClass` |
| Functions | snake_case | `my_function()` |
| Constants | UPPER_SNAKE | `MAX_SIZE` |

## Organization Tips

1. **Group by function, not type**: Keep related files together
2. **Use index files**: Export from central locations
3. **Follow language conventions**: Respect community standards
4. **Limit depth**: Avoid deeply nested structures
5. **Name descriptively**: Names should explain purpose

## Cleanup Checklist

- [ ] Remove temporary files
- [ ] Delete duplicate files
- [ ] Add .gitignore
- [ ] Create README.md
- [ ] Organize imports
- [ ] Add comments for complex files
