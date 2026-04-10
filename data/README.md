# Data Directory

This directory stores runtime data for the application.

## Structure

```
data/
├── uploads/          # Uploaded CSV files (temporary storage)
├── output/           # Generated result CSV files
└── README.md         # This file
```

## Notes

- All `.csv` files in `uploads/` and `output/` are gitignored
- Files are automatically managed by the application
- Old files can be safely deleted to free up space
