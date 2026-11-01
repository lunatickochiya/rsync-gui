'use strict';

const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));

const KEEP_ON_PRESET = [
  'program', 'useBwLimit', 'bwLimit', 'useRemoteShell', 'remoteShell', 'sshKey',
  'excludeFile', 'linkDest', 'extraArguments', 'autoModifyWindow', 'gitTrackedOnly',
  'gitIncludeRepo', 'numericIds', 'oneFileSystem', 'sparse'
];

const OPTION_GROUPS = [
  {
    title: '传输', open: true, rows: [
      { key: 'archive', type: 'bool', label: '归档模式', flag: '-a', master: true },
      { key: 'recursive', type: 'bool', label: '递归目录', flag: '-r', depends: 'archive' },
      { key: 'links', type: 'bool', label: '符号链接', flag: '-l', depends: 'archive' },
      { key: 'perms', type: 'bool', label: '权限', flag: '-p', depends: 'archive' },
      { key: 'times', type: 'bool', label: '修改时间', flag: '-t', depends: 'archive' },
      { key: 'group', type: 'bool', label: '所属组', flag: '-g', depends: 'archive' },
      { key: 'owner', type: 'bool', label: '所有者', flag: '-o', depends: 'archive' },
      { key: 'devices', type: 'bool', label: '设备与特殊文件', flag: '-D', depends: 'archive' },
      { key: 'hardLinks', type: 'bool', label: '硬链接', flag: '-H' },
      { key: 'acls', type: 'bool', label: 'ACL', flag: '-A' },
      { key: 'xattrs', type: 'bool', label: '扩展属性', flag: '-X' },
      { key: 'compress', type: 'bool', label: '压缩传输', flag: '-z' }
    ]
  },
  {
    title: '进度与冲突', open: true, rows: [
      {
        key: 'progress', type: 'choice', label: '进度显示', flag: '--progress', choices: [
          { value: 'per-file', label: '每文件' },
          { value: 'overall', label: '总体' },
          { value: 'none', label: '关闭' }
        ]
      },
      { key: 'partial', type: 'bool', label: '保留部分文件', flag: '--partial' },
      {
        key: 'existing', type: 'choice', label: '已存在文件', flag: '-u', choices: [
          { value: 'replace', label: '覆盖' },
          { value: 'skip-newer', label: '跳过较新' },
          { value: 'ignore-existing', label: '忽略已存在' }
        ]
      },
      { key: 'deleteExtraneous', type: 'bool', label: '删除目标多余文件', flag: '--delete' },
      { key: 'dryRun', type: 'bool', label: '试运行（不修改）', flag: '-n' }
    ]
  },
  {
    title: '校验与清理', open: true, rows: [
      { key: 'verifyAfter', type: 'bool', label: '复制后校验', flag: '--checksum 比较' },
      { key: 'checksum', type: 'bool', label: '按校验和比较', flag: '-c' },
      { key: 'removeSourceFiles', type: 'bool', label: '删除源文件（移动）', flag: '--remove-source-files' },
      { key: 'removeSourceDirs', type: 'bool', label: '删除空的源目录', flag: '移动后', depends: 'move' }
    ]
  },
  {
    title: '高级', open: false, rows: [
      { key: 'numericIds', type: 'bool', label: '数字 ID', flag: '--numeric-ids' },
      { key: 'oneFileSystem', type: 'bool', label: '不跨文件系统', flag: '-x' },
      { key: 'sparse', type: 'bool', label: '稀疏文件', flag: '--sparse' },
      { key: 'contentsOnly', type: 'bool', label: '只复制目录内容', flag: '源目录加 /' },
      { key: 'bwlimit', type: 'text', label: '带宽限制', flag: '--bwlimit', toggle: 'useBwLimit', placeholder: '例如 1.5M、10m、500K' },
      { key: 'sshKey', type: 'text', label: 'SSH 密钥', flag: 'ssh -i', placeholder: '~/.ssh/id_ed25519', browse: 'file' },
      { key: 'excludeFile', type: 'text', label: '排除文件', flag: '--exclude-from', placeholder: '每行一个排除模式', browse: 'file' },
      { key: 'linkDest', type: 'text', label: '硬链接基准', flag: '--link-dest', placeholder: '上一次快照目录', browse: 'dir' },
      { key: 'remoteShell', type: 'text', label: '远程 shell', flag: '-e', toggle: 'useRemoteShell', placeholder: 'ssh -o StrictHostKeyChecking=accept-new' },
      { key: 'gitTrackedOnly', type: 'bool', label: '仅 Git 跟踪文件', flag: '--files-from' },
      { key: 'gitIncludeRepo', type: 'bool', label: '包含 .git', flag: '', depends: 'git' },
      { key: 'autoModifyWindow', type: 'bool', label: 'FAT 时间容差', flag: '--modify-window=1' },
      { key: 'extraArguments', type: 'text', label: '附加参数', flag: '任意 rsync 选项', placeholder: '--exclude="*.tmp" --mkpath' },
      { key: 'program', type: 'text', label: '程序', flag: 'rsync', placeholder: 'rsync' }
    ]
  }
];

const state = {
  version: '',
  rsyncVersion: '',
  platform: '',
  listen: '',
  authEnabled: false,
  options: {},
  defaults: {},
  sources: [],
  destination: '',
  recentDestinations: [],
  presets: [],
  plan: null,
  planError: '',
  activePreset: '',
  job: null,
  running: false,
  done: false,
  logs: [],
  summary: null,
  follow: true,
  settingsName: 'rsync-web',
  starting: false
};

let els = {};
let planTimer = null;
let planSeq = 0;
let eventSource = null;
let toastTimer = null;
let browse = null;

const LEVELS = ['', 'info', 'success', 'warning', 'error'];

function init() {
  els = {
    sourceList: $('#source-list'),
    sourceCount: $('#source-count'),
    destValue: $('#dest-value'),
    presetList: $('#preset-list'),
    options: $('#options'),
    commandPreview: $('#command-preview'),
    commandState: $('#command-state'),
    warnings: $('#warnings'),
    planStatus: $('#plan-status'),
    startButton: $('#start-button'),
    modeTitle: $('#mode-title'),
    modeDesc: $('#mode-desc'),
    statSources: $('#stat-sources'),
    statOptions: $('#stat-options'),
    securityBadge: $('#security-badge'),
    rsyncVersion: $('#rsync-version'),
    conn: $('#conn'),
    setupView: $('#setup-view'),
    runView: $('#run-view'),
    runPhase: $('#run-phase'),
    runTitle: $('#run-title'),
    runClock: $('#run-clock'),
    progressFill: $('#progress-fill'),
    runPercent: $('#run-percent'),
    runRate: $('#run-rate'),
    runEta: $('#run-eta'),
    runTransferred: $('#run-transferred'),
    runFiles: $('#run-files'),
    runCurrent: $('#run-current'),
    cancelButton: $('#cancel-button'),
    backButton: $('#back-button'),
    rerunButton: $('#rerun-button'),
    logList: $('#log-list'),
    logCount: $('#log-count'),
    followLog: $('#follow-log'),
    summary: $('#summary'),
    modalRoot: $('#modal-root'),
    toast: $('#toast'),
    settingsFile: $('#settings-file')
  };

  bindEvents();
  loadBootstrap();
  setInterval(updateClock, 1000);
}

function bindEvents() {
  $('#add-source').addEventListener('click', openAddSource);
  $('#bulk-source').addEventListener('click', openBulkSource);
  $('#edit-dest').addEventListener('click', openDestination);
  $('#recent-dest').addEventListener('click', openDestination);
  $('#import-settings').addEventListener('click', () => els.settingsFile.click());
  $('#export-settings').addEventListener('click', exportSettings);
  $('#help-button').addEventListener('click', openHelp);
  els.settingsFile.addEventListener('change', importSettings);
  $('#copy-command').addEventListener('click', copyCommand);
  els.startButton.addEventListener('click', () => startJob(false));
  els.cancelButton.addEventListener('click', cancelJob);
  els.backButton.addEventListener('click', backToSetup);
  els.rerunButton.addEventListener('click', () => startJob(false));
  els.followLog.addEventListener('change', () => {
    state.follow = els.followLog.checked;
    if (state.follow) scrollLogs();
  });
  els.logList.addEventListener('scroll', () => {
    const nearBottom = els.logList.scrollHeight - els.logList.scrollTop - els.logList.clientHeight < 32;
    if (!nearBottom && state.follow) {
      state.follow = false;
      els.followLog.checked = false;
    }
  });
  els.modalRoot.addEventListener('click', (event) => {
    if (event.target === els.modalRoot) closeModal();
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      closeModal();
      return;
    }
    if (isTyping(event.target) || !els.modalRoot.classList.contains('hidden')) return;
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (!els.setupView.classList.contains('hidden')) {
      if (event.key === 'r' || event.key === 'R') {
        event.preventDefault();
        startJob(false);
      } else if (event.key === 'a') {
        event.preventDefault();
        openAddSource();
      } else if (event.key === 'A') {
        event.preventDefault();
        openBulkSource();
      } else if (event.key === 'd') {
        event.preventDefault();
        openDestination();
      } else if (event.key === 'p') {
        event.preventDefault();
        openPresetPicker();
      }
    }
    if (event.key === '?') {
      event.preventDefault();
      openHelp();
    }
  });
}

async function loadBootstrap() {
  try {
    const data = await api('/api/bootstrap');
    state.version = data.version || '';
    state.rsyncVersion = data.rsyncVersion || '';
    state.platform = data.platform || '';
    state.listen = data.listen || '';
    state.authEnabled = !!data.authEnabled;
    state.options = data.options || {};
    state.defaults = data.defaults || {};
    state.sources = Array.isArray(data.sources) ? data.sources.slice() : [];
    state.destination = data.destination || '';
    state.recentDestinations = Array.isArray(data.recentDestinations) ? data.recentDestinations.slice() : [];
    state.presets = Array.isArray(data.presets) ? data.presets.slice() : [];

    els.rsyncVersion.textContent = state.rsyncVersion || 'rsync';
    els.securityBadge.textContent = state.authEnabled ? '令牌保护' : '本机';
    els.securityBadge.classList.toggle('warn', state.authEnabled);

    renderSources();
    renderDestination();
    renderPresets();
    renderOptions();
    updateMeta();

    if (data.job && (data.job.running || data.job.done)) {
      applyJob(data.job, true);
    } else {
      showSetup();
      refreshPlan();
    }
    connectEvents();
  } catch (err) {
    setConnection('error', '连接失败');
    toast(err.message, true);
  }
}

async function api(path, options = {}) {
  const init = { method: options.method || 'GET', headers: {} };
  if (options.body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(options.body);
  }
  const response = await fetch(path, init);
  const text = await response.text();
  let data = null;
  if (text) {
    try { data = JSON.parse(text); } catch { data = text; }
  }
  if (!response.ok) {
    const message = data && data.error ? data.error : (typeof data === 'string' ? data : `HTTP ${response.status}`);
    const err = new Error(message);
    err.status = response.status;
    err.data = data;
    throw err;
  }
  return data;
}

function payload() {
  return {
    options: state.options,
    sources: state.sources,
    destination: state.destination
  };
}

function renderSources() {
  els.sourceList.replaceChildren();
  els.sourceCount.textContent = String(state.sources.length);
  if (!state.sources.length) {
    const empty = el('div', 'empty', '还没有添加源');
    els.sourceList.append(empty);
    return;
  }
  state.sources.forEach((source, index) => {
    const row = el('div', 'source-row');
    const icon = el('span', 'source-ico', isRemote(source) ? 'SSH' : 'DIR');
    const path = el('span', 'source-path', source);
    path.title = source;
    const remove = el('button', 'source-remove', '×');
    remove.type = 'button';
    remove.title = '移除这个源';
    remove.addEventListener('click', () => {
      state.sources.splice(index, 1);
      renderSources();
      updateMeta();
      schedulePlan();
    });
    row.append(icon, path, remove);
    els.sourceList.append(row);
  });
}

function renderDestination() {
  els.destValue.replaceChildren();
  if (!state.destination) {
    els.destValue.append(el('span', 'muted', '尚未选择目标'));
    return;
  }
  const span = el('span', '', state.destination);
  span.title = state.destination;
  els.destValue.append(span);
}

function renderPresets() {
  els.presetList.replaceChildren();
  const custom = el('button', 'preset' + (state.activePreset ? '' : ' active'));
  custom.type = 'button';
  const customLabel = el('span', '', '自定义');
  custom.append(customLabel, el('code', '', '当前选项'));
  custom.addEventListener('click', () => {
    state.activePreset = '';
    updatePresetHighlight();
  });
  els.presetList.append(custom);

  for (const preset of state.presets) {
    const button = el('button', 'preset' + (preset.id === state.activePreset ? ' active' : ''));
    button.type = 'button';
    button.dataset.id = preset.id;
    button.title = preset.error || preset.label || preset.name;
    if (preset.error) button.disabled = true;
    const label = el('span', '', preset.label || preset.name || preset.id);
    button.append(label, el('code', '', preset.id));
    button.addEventListener('click', () => applyPreset(preset));
    els.presetList.append(button);
  }
}

function updatePresetHighlight() {
  $$('.preset', els.presetList).forEach((button) => {
    button.classList.toggle('active', button.dataset.id === state.activePreset || (!state.activePreset && !button.dataset.id));
  });
}

function openPresetPicker() {
  const body = el('div', 'preset-list');
  for (const preset of state.presets) {
    const button = el('button', 'preset' + (preset.id === state.activePreset ? ' active' : ''));
    button.type = 'button';
    button.disabled = !!preset.error;
    button.append(el('span', '', preset.label || preset.name || preset.id), el('code', '', preset.id));
    button.addEventListener('click', () => {
      applyPreset(preset);
      closeModal();
    });
    body.append(button);
  }
  openModal({
    title: '选择预设',
    subtitle: '预设会替换传输选项，但保留程序、SSH、排除文件和附加参数。',
    body,
    footer: (bar) => {
      const close = el('button', 'btn ghost', '取消');
      close.type = 'button';
      close.addEventListener('click', closeModal);
      bar.append(close);
    }
  });
}

function applyPreset(preset) {
  if (!preset || preset.error) {
    toast(preset && preset.error ? preset.error : '预设不可用', true);
    return;
  }
  const next = Object.assign({}, preset.options);
  for (const key of KEEP_ON_PRESET) next[key] = state.options[key];
  state.options = next;
  state.activePreset = preset.id;
  renderOptions();
  renderPresets();
  updateMeta();
  schedulePlan();
  toast('已应用预设：' + (preset.label || preset.name || preset.id));
}

function rowDisabled(row) {
  if (row.depends === 'archive' && state.options.archive) return true;
  if (row.depends === 'move' && !state.options.removeSourceFiles) return true;
  if (row.depends === 'git' && !state.options.gitTrackedOnly) return true;
  return false;
}

function boolValue(row) {
  if (row.depends === 'archive') return !!(state.options.archive || state.options[row.key]);
  return !!state.options[row.key];
}

function optionNote(row) {
  if (row.key === 'deleteExtraneous' && state.options.removeSourceFiles) return '与移动冲突';
  if (row.key === 'verifyAfter' && state.options.dryRun) return '试运行下跳过';
  if (row.key === 'contentsOnly' && state.options.gitTrackedOnly) return 'Git 模式下忽略';
  if (row.key === 'removeSourceDirs' && !state.options.removeSourceFiles) return '需要先开启移动';
  if (row.key === 'gitIncludeRepo' && !state.options.gitTrackedOnly) return '需要先开启 Git 模式';
  return '';
}

function renderOptions() {
  els.options.replaceChildren();
  for (const group of OPTION_GROUPS) {
    const section = el('section', 'option-group' + (group.open ? '' : ' collapsed'));
    const head = el('button', 'group-head');
    head.type = 'button';
    head.setAttribute('aria-expanded', group.open ? 'true' : 'false');
    head.append(el('h3', '', group.title), el('span', 'group-toggle', '⌄'));
    head.addEventListener('click', () => {
      group.open = !group.open;
      section.classList.toggle('collapsed', !group.open);
      head.setAttribute('aria-expanded', group.open ? 'true' : 'false');
    });
    const body = el('div', 'group-body');
    const items = group.rows.map((row) => {
      if (row.type === 'bool') return renderBoolOption(row);
      if (row.type === 'choice') return renderChoiceOption(row);
      return renderTextOption(row);
    });
    if (items.length % 2 === 1) items[items.length - 1].classList.add('span-2');
    body.append(...items);
    section.append(head, body);
    els.options.append(section);
  }
}

function renderBoolOption(row) {
  const disabled = rowDisabled(row);
  const label = el('label', 'option-row bool' + (row.master ? ' master' : '') + (disabled ? ' disabled' : ''));
  const input = el('input');
  input.type = 'checkbox';
  input.checked = boolValue(row);
  input.disabled = disabled;
  input.addEventListener('change', () => {
    state.options[row.key] = input.checked;
    markCustom();
    renderOptions();
    updateMeta();
    schedulePlan();
  });
  const text = el('span', 'option-label');
  text.append(document.createTextNode(row.label));
  const note = optionNote(row);
  if (note) text.append(el('span', 'note', note));
  label.append(input, text);
  if (row.flag) label.append(el('code', '', row.flag));
  return label;
}

function renderChoiceOption(row) {
  const wrapper = el('label', 'option-row choice');
  wrapper.append(el('span', 'option-label', row.label));
  const select = el('select');
  for (const choice of row.choices) {
    const option = el('option', '', choice.label);
    option.value = choice.value;
    select.append(option);
  }
  select.value = state.options[row.key] || row.choices[0].value;
  select.addEventListener('change', () => {
    state.options[row.key] = select.value;
    markCustom();
    updateMeta();
    schedulePlan();
  });
  wrapper.append(select);
  if (row.flag) wrapper.append(el('code', '', row.flag));
  return wrapper;
}

function renderTextOption(row) {
  const wrapper = el('div', 'option-row text');
  const main = el('div', 'text-main');
  const head = el('div', 'text-head');
  head.append(el('span', 'option-label', row.label));
  if (row.flag) head.append(el('code', '', row.flag));
  const note = optionNote(row);
  if (note) head.append(el('span', 'note', note));

  const control = el('div', 'text-control');
  if (row.toggle) {
    const toggleLabel = el('label', 'text-toggle');
    const toggle = el('input');
    toggle.type = 'checkbox';
    toggle.checked = !!state.options[row.toggle];
    toggle.addEventListener('change', () => {
      state.options[row.toggle] = toggle.checked;
      markCustom();
      updateMeta();
      schedulePlan();
    });
    toggleLabel.append(toggle, el('span', '', '启用'));
    control.append(toggleLabel);
  }

  const input = el('input');
  input.type = 'text';
  input.value = state.options[row.key] || '';
  input.placeholder = row.placeholder || '';
  input.spellcheck = false;
  input.addEventListener('input', () => {
    state.options[row.key] = input.value;
    if (row.toggle && input.value.trim()) state.options[row.toggle] = true;
    markCustom();
    updateMeta();
    schedulePlan();
  });
  control.append(input);

  if (row.browse) {
    const browseButton = el('button', 'mini', '浏览');
    browseButton.type = 'button';
    browseButton.addEventListener('click', () => {
      openBrowser(row.browse === 'dir' ? 'dest' : 'source', (picked) => {
        state.options[row.key] = picked;
        if (row.toggle) state.options[row.toggle] = true;
        markCustom();
        renderOptions();
        updateMeta();
        schedulePlan();
      }, { selectOnly: true });
    });
    control.append(browseButton);
  }

  main.append(head, control);
  wrapper.append(main);
  return wrapper;
}

function markCustom() {
  if (state.activePreset) {
    state.activePreset = '';
    updatePresetHighlight();
  }
}

function updateMeta() {
  els.statSources.textContent = String(state.sources.length);
  els.statOptions.textContent = String(countEnabledOptions());
  const opts = state.options;
  let title = '复制文件';
  let desc = '选择源和目标，调整选项，然后开始传输。';
  if (opts.removeSourceFiles) {
    title = '移动文件';
    desc = '复制完成后删除源文件；校验通过后才删除。';
  } else if (opts.deleteExtraneous) {
    title = '镜像同步';
    desc = '目标目录会与源保持一致，源中不存在的目标文件将被删除。';
  } else if (opts.verifyAfter) {
    title = '复制并校验';
    desc = '复制完成后按校验和再检查一遍目标内容。';
  }
  if (opts.dryRun) {
    title = '试运行：' + title;
    desc = '只显示 rsync 将要执行的操作，不会修改文件。' + desc;
  }
  els.modeTitle.textContent = title;
  els.modeDesc.textContent = desc;

  let startLabel = '开始传输';
  if (opts.removeSourceFiles) startLabel = '开始移动';
  else if (opts.deleteExtraneous) startLabel = '开始同步';
  els.startButton.innerHTML = startLabel + ' <kbd>r</kbd>';
  els.startButton.disabled = !state.sources.length || !state.destination || state.starting || !!(state.job && state.job.running);
}

function countEnabledOptions() {
  let count = 0;
  for (const group of OPTION_GROUPS) {
    for (const row of group.rows) {
      if (row.type === 'bool' && boolValue(row)) count++;
      if (row.type === 'text' && row.toggle && state.options[row.toggle]) count++;
      if (row.type === 'text' && !row.toggle && state.options[row.key]) count++;
    }
  }
  return count;
}

function schedulePlan() {
  clearTimeout(planTimer);
  planTimer = setTimeout(refreshPlan, 260);
}

async function refreshPlan() {
  if (!state.sources.length || !state.destination) {
    state.plan = null;
    state.planError = '';
    els.commandPreview.textContent = '填写源和目标后显示 rsync 命令。';
    els.commandState.textContent = '';
    els.warnings.replaceChildren();
    els.planStatus.textContent = '先添加源和目标';
    els.planStatus.classList.remove('error');
    els.startButton.disabled = true;
    return;
  }
  const seq = ++planSeq;
  els.commandState.textContent = '计算中…';
  try {
    const data = await api('/api/plan', { method: 'POST', body: payload() });
    if (seq !== planSeq) return;
    state.plan = data;
    state.planError = '';
    renderPlan(data);
  } catch (err) {
    if (seq !== planSeq) return;
    state.plan = null;
    state.planError = err.message;
    els.commandPreview.textContent = '无法生成命令：' + err.message;
    els.commandState.textContent = '';
    els.warnings.replaceChildren();
    els.planStatus.textContent = err.message;
    els.planStatus.classList.add('error');
    els.startButton.disabled = true;
  }
}

function renderPlan(plan) {
  const commands = plan.commands || [];
  if (!commands.length) {
    els.commandPreview.textContent = '没有可执行的命令。';
  } else if (commands.length === 1) {
    els.commandPreview.textContent = commands[0].command;
  } else {
    els.commandPreview.textContent = commands
      .map((command) => '# ' + command.phase + '\n' + command.command)
      .join('\n\n');
  }
  els.commandState.textContent = commands.length + ' 条命令';
  els.warnings.replaceChildren();
  for (const warning of plan.warnings || []) {
    const item = el('div', 'warning' + (plan.destructive ? ' danger' : ''));
    item.textContent = '• ' + warning;
    els.warnings.append(item);
  }
  if (plan.destructive) {
    const item = el('div', 'warning danger', '• 这是会删除文件的操作，开始前需要再次确认。');
    els.warnings.append(item);
  }
  els.planStatus.textContent = plan.destructive
    ? '将执行 ' + commands.length + ' 条命令 · 需要确认删除操作'
    : '将执行 ' + commands.length + ' 条命令';
  els.planStatus.classList.remove('error');
  els.startButton.disabled = state.starting || !!(state.job && state.job.running);
}

async function startJob(confirm) {
  if (state.starting) return;
  if (!state.sources.length || !state.destination) {
    toast('请先添加源和目标', true);
    return;
  }
  state.starting = true;
  updateMeta();
  try {
    const data = await api('/api/start', {
      method: 'POST',
      body: Object.assign(payload(), { confirm: !!confirm })
    });
    state.logs = [];
    applyJob(data.job, true);
    connectEvents();
  } catch (err) {
    if (err.status === 409 && err.data && err.data.confirmRequired) {
      openConfirm(err.data);
    } else {
      toast(err.message, true);
    }
  } finally {
    state.starting = false;
    updateMeta();
  }
}

function openConfirm(plan) {
  const body = el('div');
  const intro = el('p', 'hint', '以下操作会修改或删除文件。确认目标目录和源都正确后再继续。');
  const list = el('ul', 'confirm-list');
  for (const warning of plan.warnings || []) {
    list.append(el('li', '', warning));
  }
  body.append(intro, list);
  const { footer } = openModal({
    title: '确认开始？',
    subtitle: '这一步无法自动撤销。',
    body,
    footer: (bar) => {
      const cancel = el('button', 'btn ghost', '返回检查');
      cancel.type = 'button';
      cancel.addEventListener('click', closeModal);
      const confirmButton = el('button', 'btn danger', '仍然开始');
      confirmButton.type = 'button';
      confirmButton.addEventListener('click', () => {
        closeModal();
        startJob(true);
      });
      bar.append(cancel, confirmButton);
    }
  });
  const first = footer.querySelector('button');
  if (first) first.focus();
}

function applyJob(job, show) {
  if (!job) return;
  state.job = job;
  state.running = !!job.running;
  state.done = !!job.done;
  state.logs = Array.isArray(job.logs) ? job.logs.slice() : [];
  state.summary = job.summary || null;
  if (show && (job.running || job.done)) {
    showRunView();
    renderLogs();
    renderRun();
    if (job.done) showSummary(job.summary);
  }
  updateMeta();
}

function connectEvents() {
  if (eventSource) eventSource.close();
  setConnection('connecting', '连接中');
  eventSource = new EventSource('/api/events');
  eventSource.addEventListener('open', () => setConnection('online', '已连接'));
  eventSource.addEventListener('error', () => setConnection('error', '重连中…'));
  eventSource.addEventListener('snapshot', (event) => {
    try { applyJob(JSON.parse(event.data), true); } catch (err) { console.error(err); }
  });
  eventSource.addEventListener('state', (event) => {
    try {
      const next = JSON.parse(event.data);
      if (!state.job) state.job = {};
      state.job.state = next;
      state.job.running = state.running;
      state.job.done = state.done;
      renderRun();
    } catch (err) { console.error(err); }
  });
  eventSource.addEventListener('log', (event) => {
    try { appendLog(JSON.parse(event.data)); } catch (err) { console.error(err); }
  });
  eventSource.addEventListener('done', (event) => {
    try {
      const summary = JSON.parse(event.data);
      if (!state.job) state.job = {};
      state.job.summary = summary;
      state.job.running = false;
      state.job.done = true;
      state.running = false;
      state.done = true;
      state.summary = summary;
      renderRun();
      showSummary(summary);
      updateMeta();
    } catch (err) { console.error(err); }
  });
}

function showRunView() {
  els.setupView.classList.add('hidden');
  els.runView.classList.remove('hidden');
  window.scrollTo({ top: 0, behavior: 'smooth' });
}

function showSetup() {
  els.runView.classList.add('hidden');
  els.setupView.classList.remove('hidden');
}

function backToSetup() {
  state.done = false;
  state.summary = null;
  showSetup();
  els.summary.classList.add('hidden');
  updateMeta();
  refreshPlan();
}

function renderRun() {
  if (!state.job) return;
  const job = state.job;
  const st = job.state || {};
  const summary = job.summary || state.summary;
  const done = !!job.done;
  const cancelled = summary && summary.Cancelled;
  const failed = summary && !summarySuccess(summary) && !cancelled;
  let phase = st.Phase || (done ? '完成' : '复制');
  let title = '正在' + (phase === '复制' || phase === '' ? '传输' : phase);
  if (done) {
    title = cancelled ? '已取消' : (failed ? '传输未完全成功' : '传输完成');
    phase = cancelled ? '已取消' : (failed ? '未完成' : '完成');
  }
  els.runPhase.textContent = phase;
  els.runTitle.textContent = title;

  const percent = done && summarySuccess(summary) ? 100 : Number(st.Percent || 0);
  els.runPercent.textContent = st.Indeterminate ? '…' : Math.round(percent) + '%';
  els.progressFill.classList.toggle('indeterminate', !done && !!st.Indeterminate);
  els.progressFill.style.width = Math.max(0, Math.min(100, percent)) + '%';
  els.runRate.textContent = st.Rate || formatRate(st.Speed) || '—';
  els.runEta.textContent = st.ETA || '—';
  els.runTransferred.textContent = st.Transferred || '—';
  if (st.FilesTotal > 0) {
    els.runFiles.textContent = st.FilesDone + '/' + st.FilesTotal;
  } else if (st.TransferCount > 1) {
    els.runFiles.textContent = (st.TransferIndex || 0) + '/' + st.TransferCount;
  } else {
    els.runFiles.textContent = st.FilesDone ? String(st.FilesDone) : '—';
  }
  els.runCurrent.textContent = st.CurrentFile || (done ? '没有更多文件。' : '等待 rsync…');
  if (done && summary) els.runClock.textContent = formatDuration((summary.Duration || 0) / 1e6);

  els.cancelButton.classList.toggle('hidden', done);
  els.cancelButton.disabled = !!job.cancelling;
  els.cancelButton.textContent = job.cancelling ? '正在取消…' : '取消传输';
  els.backButton.classList.toggle('hidden', !done);
  els.rerunButton.classList.toggle('hidden', !done);
  updateClock();
  if (done) showSummary(summary);
}

function updateClock() {
  if (!state.running || !state.job) return;
  const started = Date.parse(state.job.startedAt || '');
  if (Number.isFinite(started)) {
    els.runClock.textContent = formatDuration(Date.now() - started);
  } else if (state.job.state && state.job.state.Elapsed) {
    els.runClock.textContent = formatDuration(state.job.state.Elapsed / 1e6);
  }
}

function summarySuccess(summary) {
  return !!summary && !summary.Cancelled && !summary.Aborted &&
    (summary.Failed || 0) === 0 &&
    (summary.VerifyFailed || 0) === 0 &&
    (summary.Differences || 0) === 0 &&
    (summary.RemovalFailed || 0) === 0;
}

function showSummary(summary) {
  if (!summary) {
    els.summary.classList.add('hidden');
    return;
  }
  const success = summarySuccess(summary);
  els.summary.className = 'summary' + (success ? '' : ' fail');
  els.summary.replaceChildren();
  const title = el('h3', '', success ? '✓ 全部完成' : (summary.Cancelled ? '已取消' : '传输未完全成功'));
  const text = el('p', '', summary.Err || (success
    ? '所有传输和校验均已通过。'
    : '请查看上面的日志，确认失败或差异的文件。'));
  const grid = el('div', 'summary-grid');
  const stats = [
    ['完成', summary.OK || 0],
    ['警告', summary.Warnings || 0],
    ['失败', summary.Failed || 0],
    ['校验差异', (summary.Differences || 0) + (summary.VerifyFailed || 0)],
    ['删除源', summary.SourcesRemoved || 0],
    ['用时', formatDuration((summary.Duration || 0) / 1e6)]
  ];
  for (const [label, value] of stats) {
    const cell = el('div');
    cell.append(el('span', '', String(value)), el('small', '', label));
    grid.append(cell);
  }
  els.summary.append(title, text, grid);
  els.summary.classList.remove('hidden');
}

function renderLogs() {
  els.logList.replaceChildren();
  const fragment = document.createDocumentFragment();
  for (const line of state.logs) fragment.append(logElement(line));
  els.logList.append(fragment);
  els.logCount.textContent = state.logs.length + ' 行';
  if (state.follow) scrollLogs();
}

function appendLog(line) {
  state.logs.push(line);
  if (state.logs.length > 5000) state.logs.splice(0, state.logs.length - 5000);
  els.logList.append(logElement(line));
  els.logCount.textContent = state.logs.length + ' 行';
  if (state.follow) scrollLogs();
}

function logElement(line) {
  const row = el('div', 'log-line ' + (LEVELS[line.Level] || ''));
  row.append(
    el('span', 'log-time', formatLogTime(line.Time)),
    el('span', 'log-text', line.Text || '')
  );
  return row;
}

function scrollLogs() {
  els.logList.scrollTop = els.logList.scrollHeight;
}

async function cancelJob() {
  els.cancelButton.disabled = true;
  try {
    await api('/api/cancel', { method: 'POST', body: {} });
    if (state.job) state.job.cancelling = true;
    renderRun();
  } catch (err) {
    toast(err.message, true);
    els.cancelButton.disabled = false;
  }
}

function openAddSource() {
  const body = el('div');
  const field = el('div', 'field');
  const label = el('label', '', '本地路径或远程路径');
  const row = el('div', 'field-row');
  const input = el('input');
  input.type = 'text';
  input.placeholder = '/home/me/Photos 或 user@nas:/volume1/photos';
  row.append(input);
  const browseButton = el('button', 'btn ghost', '浏览本机');
  browseButton.type = 'button';
  browseButton.addEventListener('click', () => {
    closeModal();
    openBrowser('source', (picked) => addSources([picked]));
  });
  row.append(browseButton);
  field.append(label, row, el('div', 'hint', '支持 user@host:/path 和 sftp://user@host:port/path。本机路径可以用浏览器选择。'));
  body.append(field);

  const { footer } = openModal({
    title: '添加源',
    subtitle: '可以添加多个源，rsync 会按所在主机分组执行。',
    body,
    footer: (bar) => {
      const cancel = el('button', 'btn ghost', '取消');
      cancel.type = 'button';
      cancel.addEventListener('click', closeModal);
      const add = el('button', 'btn primary', '添加');
      add.type = 'button';
      const submit = () => {
        const value = input.value.trim();
        if (!value) { toast('请输入路径', true); input.focus(); return; }
        addSources([value]);
        closeModal();
      };
      add.addEventListener('click', submit);
      input.addEventListener('keydown', (event) => { if (event.key === 'Enter') submit(); });
      bar.append(cancel, add);
      setTimeout(() => input.focus(), 0);
    }
  });
  return footer;
}

function openBulkSource() {
  const body = el('div');
  const field = el('div', 'field');
  field.append(el('label', '', '每行一个源路径'));
  const textarea = el('textarea');
  textarea.placeholder = '/home/me/Photos\n/media/usb/backup\nuser@nas:/volume1/photos\nsftp://nas:2222/volume1/archive';
  field.append(textarea);
  body.append(field);
  openModal({
    title: '批量添加源',
    subtitle: '空行会忽略，重复路径会自动去重。',
    body,
    footer: (bar) => {
      const cancel = el('button', 'btn ghost', '取消');
      cancel.type = 'button';
      cancel.addEventListener('click', closeModal);
      const add = el('button', 'btn primary', '添加全部');
      add.type = 'button';
      add.addEventListener('click', () => {
        const values = textarea.value.split(/\r?\n/).map((value) => value.trim()).filter(Boolean);
        if (!values.length) { toast('至少输入一行路径', true); return; }
        addSources(values);
        closeModal();
      });
      bar.append(cancel, add);
      setTimeout(() => textarea.focus(), 0);
    }
  });
}

function addSources(values) {
  let added = 0;
  for (const value of values) {
    if (!state.sources.includes(value)) {
      state.sources.push(value);
      added++;
    }
  }
  renderSources();
  updateMeta();
  schedulePlan();
  if (added) toast('已添加 ' + added + ' 个源');
  else toast('这些路径已经在列表中了', true);
}

function openDestination() {
  const body = el('div');
  const field = el('div', 'field');
  field.append(el('label', '', '目标目录或远程目标'));
  const row = el('div', 'field-row');
  const input = el('input');
  input.type = 'text';
  input.value = state.destination || '';
  input.placeholder = '/mnt/d/backup 或 user@nas:/volume1/backup';
  row.append(input);
  const browseButton = el('button', 'btn ghost', '浏览本机');
  browseButton.type = 'button';
  browseButton.addEventListener('click', () => {
    closeModal();
    openBrowser('dest', (picked) => setDestination(picked));
  });
  row.append(browseButton);
  field.append(row, el('div', 'hint', '目标不存在时会自动创建；远程目标不会在网页里浏览。'));
  body.append(field);

  if (state.recentDestinations.length) {
    body.append(el('label', 'small muted', '最近使用'));
    const list = el('div', 'recent-list');
    for (const recent of state.recentDestinations) {
      const item = el('button', 'recent-item', recent);
      item.type = 'button';
      item.addEventListener('click', () => { setDestination(recent); closeModal(); });
      list.append(item);
    }
    body.append(list);
  }

  openModal({
    title: '选择目标',
    subtitle: '目标可以是本地目录，也可以是 SSH 远程目录。',
    body,
    footer: (bar) => {
      const cancel = el('button', 'btn ghost', '取消');
      cancel.type = 'button';
      cancel.addEventListener('click', closeModal);
      const save = el('button', 'btn primary', '使用这个目标');
      save.type = 'button';
      const submit = () => {
        const value = input.value.trim();
        if (!value) { toast('请输入目标目录', true); input.focus(); return; }
        setDestination(value);
        closeModal();
      };
      save.addEventListener('click', submit);
      input.addEventListener('keydown', (event) => { if (event.key === 'Enter') submit(); });
      bar.append(cancel, save);
      setTimeout(() => input.focus(), 0);
    }
  });
}

function setDestination(value) {
  state.destination = value;
  renderDestination();
  updateMeta();
  refreshPlan();
}

function openBrowser(mode, onPick, options = {}) {
  browse = { mode, onPick, data: null, selectOnly: !!options.selectOnly };
  const body = el('div');
  const pathRow = el('div', 'browse-path');
  const pathInput = el('input');
  pathInput.type = 'text';
  pathInput.placeholder = '输入路径后回车';
  const go = el('button', 'btn ghost', '跳转');
  go.type = 'button';
  pathRow.append(pathInput, go);
  const hint = el('div', 'hint', mode === 'dest'
    ? '只能选择目录。点击目录进入，底部按钮选择当前目录。'
    : '点击目录进入；点击文件直接添加为源；底部按钮添加当前目录。');
  const list = el('div', 'browse-list');
  body.append(pathRow, hint, list);

  const { footer } = openModal({
    title: mode === 'dest' ? '选择目标目录' : '浏览本机路径',
    subtitle: '运行服务的主机上的文件系统',
    wide: true,
    body,
    footer: (bar) => {
      const up = el('button', 'btn ghost', '上一级');
      up.type = 'button';
      up.addEventListener('click', () => {
        if (browse.data && browse.data.parent) loadBrowse(browse.data.parent);
      });
      const home = el('button', 'btn ghost', '主目录');
      home.type = 'button';
      home.addEventListener('click', () => {
        if (browse.data && browse.data.home) loadBrowse(browse.data.home);
      });
      const cancel = el('button', 'btn ghost', '取消');
      cancel.type = 'button';
      cancel.addEventListener('click', closeModal);
      const pick = el('button', 'btn primary', mode === 'dest' ? '选择当前目录' : '添加当前目录');
      pick.type = 'button';
      const chooseCurrent = () => {
        if (!browse.data) return;
        const path = browse.data.path;
        closeModal();
        browse.onPick(path);
      };
      pick.addEventListener('click', chooseCurrent);
      bar.append(up, home, cancel, pick);
    }
  });

  const loadBrowse = async (path) => {
    list.replaceChildren(el('div', 'browse-empty', '读取中…'));
    try {
      const data = await api('/api/browse' + (path ? '?path=' + encodeURIComponent(path) : ''));
      if (!browse) return;
      browse.data = data;
      pathInput.value = data.path || '';
      list.replaceChildren();
      if (!data.entries || !data.entries.length) {
        list.append(el('div', 'browse-empty', '这个目录是空的'));
      } else {
        for (const entry of data.entries) {
          const button = el('button', 'browse-entry' + (entry.dir ? '' : ' file'));
          button.type = 'button';
          button.append(el('span', 'ico', entry.dir ? '▸' : '·'), el('span', '', entry.name));
          if (!entry.dir) button.append(el('span', 'size', formatBytes(entry.size)));
          button.addEventListener('click', () => {
            if (entry.dir) {
              loadBrowse(entry.path);
            } else if (browse.mode === 'source') {
              closeModal();
              browse.onPick(entry.path);
            } else {
              toast('目标必须是目录', true);
            }
          });
          list.append(button);
        }
      }
      if (data.truncated) list.append(el('div', 'browse-empty', '目录条目太多，只显示前 2000 个'));
    } catch (err) {
      list.replaceChildren(el('div', 'browse-empty', err.message));
    }
  };

  go.addEventListener('click', () => loadBrowse(pathInput.value.trim()));
  pathInput.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      loadBrowse(pathInput.value.trim());
    }
  });
  const startPath = mode === 'dest' ? (state.destination && !isRemote(state.destination) ? state.destination : '') : '';
  loadBrowse(startPath);
  return footer;
}

function openHelp() {
  const body = el('div', 'help-grid');
  body.innerHTML = `
    <div>
      <h4>基本操作</h4>
      <ul>
        <li><code>a</code> 添加源；<code>A</code> 批量粘贴</li>
        <li><code>d</code> 设置目标；<code>p</code> 选择预设</li>
        <li><code>r</code> 开始；<code>Esc</code> 关闭弹窗</li>
        <li>左边的 <b>浏览本机</b> 会读取运行服务的主机文件系统</li>
      </ul>
    </div>
    <div>
      <h4>安全</h4>
      <ul>
        <li>监听 127.0.0.1 时默认不需要令牌</li>
        <li>监听 0.0.0.0 时自动生成访问令牌</li>
        <li>令牌保存在 HttpOnly Cookie；也可以使用 Bearer</li>
        <li>公网使用请走 SSH 隧道或 HTTPS 反向代理</li>
      </ul>
    </div>
    <div>
      <h4>会删除文件的操作</h4>
      <p><code>--delete</code>、<code>--remove-source-files</code> 会在开始前弹出第二次确认。</p>
      <p>“移动并校验”只有校验通过后才删除源文件。</p>
    </div>
    <div>
      <h4>限制</h4>
      <p>服务不处理 SSH 密码和未知主机密钥交互。请提前配置 SSH 密钥或 ssh-agent。</p>
      <p>同一时间只运行一个传输；远程到远程复制和 rsync daemon 不支持。</p>
    </div>`;
  openModal({
    title: '帮助',
    subtitle: 'rsync web 的常用操作与限制',
    wide: true,
    body,
    footer: (bar) => {
      const close = el('button', 'btn primary', '知道了');
      close.type = 'button';
      close.addEventListener('click', closeModal);
      bar.append(close);
    }
  });
}

async function importSettings() {
  const file = els.settingsFile.files && els.settingsFile.files[0];
  if (!file) return;
  try {
    const text = await file.text();
    const response = await fetch('/api/settings/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: text
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error || '导入失败');
    state.options = data.options;
    state.activePreset = '';
    renderOptions();
    renderPresets();
    updateMeta();
    schedulePlan();
    toast('已导入设置' + (data.name ? '：' + data.name : ''));
  } catch (err) {
    toast(err.message, true);
  } finally {
    els.settingsFile.value = '';
  }
}

async function exportSettings() {
  try {
    const response = await fetch('/api/settings/export', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ options: state.options, name: state.activePreset || 'rsync-web' })
    });
    if (!response.ok) {
      const data = await response.json().catch(() => ({}));
      throw new Error(data.error || '导出失败');
    }
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'rsync-web-settings.json';
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 2000);
    toast('设置已导出');
  } catch (err) {
    toast(err.message, true);
  }
}

async function copyCommand() {
  const text = els.commandPreview.textContent || '';
  if (!text) return;
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const area = document.createElement('textarea');
    area.value = text;
    document.body.append(area);
    area.select();
    document.execCommand('copy');
    area.remove();
  }
  toast('命令已复制');
}

function openModal({ title, subtitle, wide, body, footer }) {
  els.modalRoot.replaceChildren();
  const modal = el('div', 'modal' + (wide ? ' wide' : ''));
  const head = el('div', 'modal-head');
  const titleBox = el('div');
  titleBox.append(el('h3', '', title));
  if (subtitle) titleBox.append(el('p', '', subtitle));
  const close = el('button', 'modal-close', '×');
  close.type = 'button';
  close.title = '关闭';
  close.addEventListener('click', closeModal);
  head.append(titleBox, close);

  const bodyEl = el('div', 'modal-body');
  if (typeof body === 'string') bodyEl.innerHTML = body;
  else if (body) bodyEl.append(body);

  modal.append(head, bodyEl);
  let footerEl = null;
  if (footer) {
    footerEl = el('div', 'modal-foot');
    footer(footerEl);
    modal.append(footerEl);
  }
  els.modalRoot.append(modal);
  els.modalRoot.classList.remove('hidden');
  els.modalRoot.setAttribute('aria-hidden', 'false');
  return { body: bodyEl, footer: footerEl, close: closeModal };
}

function closeModal() {
  if (els.modalRoot.classList.contains('hidden')) return;
  els.modalRoot.classList.add('hidden');
  els.modalRoot.setAttribute('aria-hidden', 'true');
  els.modalRoot.replaceChildren();
  browse = null;
}

function toast(message, bad = false) {
  clearTimeout(toastTimer);
  els.toast.textContent = message;
  els.toast.classList.toggle('bad', !!bad);
  els.toast.classList.add('show');
  toastTimer = setTimeout(() => els.toast.classList.remove('show'), 3200);
}

function setConnection(kind, text) {
  els.conn.className = 'conn ' + (kind === 'online' ? 'online' : kind === 'error' ? 'error' : 'offline');
  const label = els.conn.querySelector('span');
  if (label) label.textContent = text;
}

function isTyping(target) {
  if (!target) return false;
  const tag = target.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable;
}

function isRemote(path) {
  return /^(sftp|ssh):\/\//i.test(path) || /^[^/\\]+@[^/\\]+:/.test(path) || /^[^/\\]+:[^/\\]/.test(path);
}

function el(tag, className = '', text = '') {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== '') node.textContent = text;
  return node;
}

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  if (bytes < 1024) return Math.round(bytes) + ' B';
  const units = ['KiB', 'MiB', 'GiB', 'TiB'];
  let value = bytes / 1024;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index++;
  }
  return value.toFixed(value >= 10 ? 1 : 2) + ' ' + units[index];
}

function formatRate(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '';
  return formatBytes(bytes) + '/s';
}

function formatDuration(milliseconds) {
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '00:00';
  const total = Math.floor(milliseconds / 1000);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  if (hours > 0) return String(hours).padStart(2, '0') + ':' + String(minutes).padStart(2, '0') + ':' + String(seconds).padStart(2, '0');
  return String(minutes).padStart(2, '0') + ':' + String(seconds).padStart(2, '0');
}

function formatLogTime(value) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return '--:--:--';
  return date.toLocaleTimeString('zh-CN', { hour12: false });
}

document.addEventListener('DOMContentLoaded', init);
