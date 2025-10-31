# MoonBit TUI Development Roadmap
## Complete User Journey and Screen Architecture

### **PRINCIPLE**: TUI should leverage the working CLI functionality (scan, clean, dry-run) that we just completed

---

## **SCREEN 1: Welcome/Launcher Screen**
**Purpose**: First impression, menu selection, and system overview
**Layout**: 
- ASCII Art Header (MoonBit logo)
- Quick stats (if scan exists): "Last scan: 21,408 files (1.8GB cleanable)"
- Menu options with keyboard shortcuts

**Menu Options**:
1. **Quick Scan** (S) - Run comprehensive scan immediately
2. **Review Previous Scan** (R) - Show last scan results  
3. **Clean System** (C) - Launch cleaning interface
4. **Settings** (G) - Configuration menu
5. **Exit** (Q) - Quit application

**Process**: Static menu, no background scanning, fast loading

---

## **SCREEN 2: Scan Progress Screen**
**Purpose**: Real-time scanning feedback, user confidence building
**Layout**:
- Progress header: "Scanning System..." 
- Current phase display: "Checking Pacman cache..."
- Progress bar with percentage
- Live stats: "Found 364 files so far (690.6MB)"

**Scanning Phases**:
1. **Initializing** → "Setting up scan environment..."
2. **Pacman Cache** → "Scanning /var/cache/pacman/pkg..."
3. **AUR Caches** → "Checking yay/paru caches..."  
4. **User Cache** → "Scanning ~/.cache directories..."
5. **Thumbnails** → "Finding thumbnail cache..."
6. **Browser Cache** → "Searching browser cache..."
7. **Finalizing** → "Processing results..."

**Process**: 
- Calls CLI: `moonbit scan` in background
- Shows live progress updates
- Captures output for display
- Handles errors gracefully (skip inaccessible paths)

---

## **SCREEN 3: Scan Results Summary**
**Purpose**: Show comprehensive scan results, build value proposition
**Layout**:
- Large summary header: "Scan Complete! 🎉"
- Key metrics in prominent boxes:
  - **21,408 files found**
  - **1.8 GB can be cleaned**
  - **9 categories scanned**
- Breakdown table with categories and sizes

**Content**:
```
✅ SCAN RESULTS SUMMARY
========================
📁 Pacman Cache:     690.6 MB (364 files)
📁 Yay Cache:        171.4 MB (13 files)  
📁 Paru Cache:        14.1 MB (20 files)
📁 Thumbnails:        19.4 MB (910 files)
📁 Browser Cache:    897.0 MB (19,191 files)

💡 RECOMMENDATION: Clean all selected categories
```

**Process**: 
- Reads scan results from CLI session cache: `~/.cache/moonbit/scan_results.json`
- Parse and format JSON data for display
- Highlight space savings value proposition

---

## **SCREEN 4: Category Selection Screen**
**Purpose**: User-controlled selection of what to clean
**Layout**:
- Title: "Select Categories to Clean"
- Checkbox list with category details
- Select all/none buttons
- Total calculation updates in real-time

**Categories** (with checkboxes):
```
☐ Pacman Cache     (690.6 MB) [HIGH] - Safe to clean
☐ Yay Cache        (171.4 MB) [HIGH] - Can be regrown
☐ Paru Cache        (14.1 MB) [HIGH] - Can be regrown  
☐ Thumbnails        (19.4 MB) [MEDIUM] - Will regenerate
☐ Browser Cache    (897.0 MB) [HIGH] - Will regenerate

Total Selected: 0 files (0 B)
```

**Process**:
- Load category list from scan results
- Show risk levels and user guidance
- Allow selective cleaning (advanced users)
- Update running totals as user selects

---

## **SCREEN 5: Safety Confirmation Screen**
**Purpose**: Final safety check, prevent accidental data loss
**Layout**:
- Warning header with dramatic styling
- Clear summary of what will be deleted
- Large confirmation buttons

**Content**:
```
⚠️  FINAL CONFIRMATION REQUIRED ⚠️
=================================

You are about to permanently delete:
• 21,408 files (1.8 GB)
• Package caches (can be regenerated)
• Browser cache (will be recreated)
• Thumbnail cache (will rebuild)

This action CANNOT be undone!

[Cancel]           [Confirm & Clean]
```

**Process**:
- Show exact files that will be deleted
- Include count and size warnings
- Require explicit confirmation
- Provide cancel option

---

## **SCREEN 6: Cleaning Progress Screen**
**Purpose**: Real-time cleaning feedback
**Layout**:
- Cleaning header with progress
- Current operation display
- Deletion counter and speed

**Progress Display**:
```
🧹 CLEANING IN PROGRESS...
==========================
Current: Deleting pacman cache files...
Progress: 123 / 21,408 files (0.6%)
Speed: 150 files/second
Freed: 1.7 GB so far

💾 Categories:
☐ Pacman Cache  (processing...)
☐ Yay Cache     (queued)
☐ Browser Cache (queued)
```

**Process**:
- Execute actual cleaning via CLI: `moonbit clean --force`
- Show real-time progress from CLI output
- Display files being deleted
- Handle errors gracefully
- Show space being freed

---

## **SCREEN 7: Cleaning Complete Screen**
**Purpose**: Celebration and summary of accomplishments
**Layout**:
- Success header with checkmarks
- Clear statistics of what was accomplished
- Refreshed system stats

**Content**:
```
✅ CLEANING COMPLETE! 🎉
========================

🗑️  Successfully deleted: 21,408 files
💾 Space freed: 1.8 GB
⚡ Cleaning speed: 2.3 seconds

📊 BREAKDOWN:
• Pacman Cache: 690.6 MB freed
• Browser Cache: 897.0 MB freed  
• AUR Caches: 185.5 MB freed
• Thumbnails: 19.4 MB freed

🚀 Your system is now optimized!
```

**Process**:
- Calculate and display space saved
- Show performance metrics
- Provide "Continue" option to main menu

---

## **SCREEN 8: Settings Configuration Screen**
**Purpose**: User customization and preferences
**Layout**:
- Settings categories in menu format
- Current values displayed
- Apply/save functionality

**Settings Categories**:
```
Settings Menu
=============
← Back to Main Menu

Scan Settings
• Max Depth: [3] (currently: 3)
• Ignore Patterns: [node_modules, .git] 
• Enable All Categories: [true]

Cleaning Settings  
• Default to Dry-Run: [true]
• Verify Before Clean: [true]
• Create Backups: [false]

Display Settings
• Theme: [Default] ▼
• Confirmations: [Strict] ▼
```

**Process**:
- Load current config from `~/.config/moonbit/config.toml`
- Allow real-time editing
- Save changes to config file
- Apply immediately or on next scan

---

## **SCREEN 9: About/Help Screen**
**Purpose**: Information, help, and credits
**Layout**:
- Application information
- Usage instructions
- Contact/support info

**Content**:
```
About MoonBit
=============
Version: 1.0.0
Description: System cleaning TUI for Linux

Features:
• Comprehensive system scanning
• Safe dry-run cleaning
• Package cache management
• User cache optimization
• Interactive TUI interface

Usage:
• S: Quick Scan
• C: Clean System  
• R: Review Results
• G: Settings
• Q: Quit

For more info: github.com/Nomadcxx/moonbit
```

---

## **ERROR HANDLING FLOW**

### **Permission Errors**
- Detect when sudo is required
- Show clear message: "This requires sudo access"
- Provide instructions: "Run with: sudo moonbit"
- Option to continue with user-space only

### **Empty Scan Results**
- Detect no files found
- Show "System already clean!" message
- Provide option to run deeper scan

### **Cleaning Failures**
- Handle individual file deletion errors
- Continue with remaining files
- Show summary: "Cleaned 95% successfully"

---

## **KEYBOARD NAVIGATION**

### **Global Keys**:
- **Esc**: Back to previous screen
- **Q**: Quit application
- **G**: Go to settings
- **Ctrl+C**: Force quit

### **Navigation Keys**:
- **Up/Down**: Navigate menu items
- **Enter**: Select/activate
- **Space**: Toggle checkbox
- **A**: Select all
- **N**: Select none

---

## **INTEGRATION POINTS**

### **CLI Integration**:
- Scan: `moonbit scan` → reads cache results
- Clean: `moonbit clean --force` → captures output
- Dry-run: `moonbit clean --dry-run` → shows preview

### **Cache Integration**:
- Session cache: `~/.cache/moonbit/scan_results.json`
- Config file: `~/.config/moonbit/config.toml`
- Log file: `~/.cache/moonbit/cleaning.log`

### **File System Integration**:
- Safe path validation
- Permission checking
- Backup creation (optional)
- Recovery mechanisms

---

## **VISUAL STYLING - sysc-greet Installer Pattern**

### **Design Principles**:
- **Dark theme** with bright accents (cyan/green for success, red for warnings)
- **Clean geometric layouts** with plenty of whitespace
- **ASCII art headers** for branding and visual appeal
- **Progressive disclosure** - show information as needed
- **Status indicators** with clear visual hierarchy
- **Button-heavy interface** with obvious actions

### **Color Scheme** (inspired by sysc-greet):
- **Background**: Dark gray/black
- **Primary text**: White/light gray  
- **Headers**: Bright cyan (#00FFFF)
- **Success**: Bright green (#00FF00)
- **Warnings**: Bright yellow (#FFFF00)
- **Errors**: Bright red (#FF0000)
- **Accents**: Blue/purple highlights

### **Typography**:
- **Large ASCII headers** for brand identity
- **Clear section dividers** using borders and lines
- **Prominent statistics** in large, bold text
- **Consistent indentation** and alignment
- **Clear hierarchy** with size differences

### **Layout Patterns**:
- **Full-screen takeover** (tea.WithAltScreen())
- **Centered content** with max-width constraints
- **Progress bars** with percentage indicators
- **Button layouts** with clear yes/no options
- **Status columns** with aligned data
- **Modal confirmations** for dangerous actions

1. **Speed**: Fast scanning, responsive UI
2. **Safety**: Multiple confirmations, dry-run default
3. **Transparency**: Show exactly what's being deleted
4. **Feedback**: Real-time progress and results
5. **Reversibility**: Clear undo options where possible
6. **Educational**: Help users understand what can be cleaned

This roadmap provides the complete vision for a production-ready TUI that leverages our proven CLI functionality while providing an intuitive, safe user experience.