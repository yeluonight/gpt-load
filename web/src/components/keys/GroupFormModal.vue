<script setup lang="ts">
import { keysApi } from "@/api/keys";
import { settingsApi } from "@/api/settings";
import ProxyKeysInput from "@/components/common/ProxyKeysInput.vue";
import type { Group, GroupConfigOption, UpstreamInfo } from "@/types/models";
import { Add, Close, HelpCircleOutline, Remove } from "@vicons/ionicons5";
import {
  NButton,
  NCard,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSwitch,
  NTooltip,
  useMessage,
  type FormRules,
} from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

interface Props {
  show: boolean;
  group?: Group | null;
}

interface Emits {
  (e: "update:show", value: boolean): void;
  (e: "success", value: Group): void;
  (e: "switchToGroup", groupId: number): void;
}

// 配置项类型
interface ConfigItem {
  key: string;
  value: number | string | boolean;
}

interface ModelRateLimitItem {
  model: string;
  rpm: number | null;
  tpm: number | null;
  request_limit: RequestLimitForm;
}

type ResetMode = "interval" | "daily";

interface RequestLimitForm {
  max_requests: number | null;
  reset_mode: ResetMode;
  interval_minutes: number | null;
  reset_time: string;
}

type KeyRequestLimitForm = RequestLimitForm;

interface ProxyPoolItemForm {
  url: string;
  disabled: boolean;
  permanent_disabled: boolean;
  notes: string;
  last_test_ok: boolean | null;
  last_test_error: string;
  last_test_at: string;
  testing: boolean;
}

// Header规则类型
interface HeaderRuleItem {
  key: string;
  value: string;
  action: "set" | "remove";
}

const props = withDefaults(defineProps<Props>(), {
  group: null,
});

const emit = defineEmits<Emits>();

const { t } = useI18n();
const message = useMessage();
const loading = ref(false);
const formRef = ref();
const modelRedirectTip = `{
  "gpt-5": "gpt-5-2025-08-07",
  "gemini-2.5-flash": "gemini-2.5-flash-preview-09-2025"
}`;

function createRequestLimitDefaults(): RequestLimitForm {
  return {
    max_requests: null,
    reset_mode: "daily",
    interval_minutes: 1440,
    reset_time: "00:00",
  };
}

// 表单数据接口
interface GroupFormData {
  name: string;
  display_name: string;
  description: string;
  upstreams: UpstreamInfo[];
  channel_type: "anthropic" | "gemini" | "openai" | "openai-response";
  sort: number;
  test_model: string;
  validation_endpoint: string;
  param_overrides: string;
  model_redirect_rules: string;
  model_redirect_strict: boolean;
  config: Record<string, unknown>;
  configItems: ConfigItem[];
  model_rate_limits: ModelRateLimitItem[];
  key_request_limit: KeyRequestLimitForm;
  proxy_pool_items: ProxyPoolItemForm[];
  proxy_pool_new_url: string;
  proxy_pool_cooldown_seconds: number;
  proxy_pool_auto_enable_interval_seconds: number;
  header_rules: HeaderRuleItem[];
  proxy_keys: string;
  group_type?: string;
}

// 表单数据
const formData = reactive<GroupFormData>({
  name: "",
  display_name: "",
  description: "",
  upstreams: [
    {
      url: "",
      weight: 1,
    },
  ] as UpstreamInfo[],
  channel_type: "openai",
  sort: 1,
  test_model: "",
  validation_endpoint: "",
  param_overrides: "",
  model_redirect_rules: "",
  model_redirect_strict: false,
  config: {},
  configItems: [] as ConfigItem[],
  model_rate_limits: [] as ModelRateLimitItem[],
  key_request_limit: createRequestLimitDefaults(),
  proxy_pool_items: [] as ProxyPoolItemForm[],
  proxy_pool_new_url: "",
  proxy_pool_cooldown_seconds: 60,
  proxy_pool_auto_enable_interval_seconds: 60,
  header_rules: [] as HeaderRuleItem[],
  proxy_keys: "",
  group_type: "standard",
});

const channelTypeOptions = ref<{ label: string; value: string }[]>([]);
const configOptions = ref<GroupConfigOption[]>([]);
const channelTypesFetched = ref(false);
const configOptionsFetched = ref(false);

// 跟踪用户是否已手动修改过字段（仅在新增模式下使用）
const userModifiedFields = ref({
  test_model: false,
  upstream: false,
});

// 根据渠道类型动态生成占位符提示
const testModelPlaceholder = computed(() => {
  switch (formData.channel_type) {
    case "openai":
    case "openai-response":
      return "gpt-4.1-nano";
    case "gemini":
      return "gemini-2.0-flash-lite";
    case "anthropic":
      return "claude-3-haiku-20240307";
    default:
      return t("keys.enterModelName");
  }
});

const upstreamPlaceholder = computed(() => {
  switch (formData.channel_type) {
    case "openai":
    case "openai-response":
      return "https://api.openai.com";
    case "gemini":
      return "https://generativelanguage.googleapis.com";
    case "anthropic":
      return "https://api.anthropic.com";
    default:
      return t("keys.enterUpstreamUrl");
  }
});

const validationEndpointPlaceholder = computed(() => {
  switch (formData.channel_type) {
    case "openai":
      return "/v1/chat/completions";
    case "openai-response":
      return "/v1/responses";
    case "anthropic":
      return "/v1/messages";
    case "gemini":
      return ""; // Gemini 不显示此字段
    default:
      return t("keys.enterValidationPath");
  }
});

const resetModeOptions = computed(() => [
  { label: t("keys.resetDaily"), value: "daily" },
  { label: t("keys.resetByInterval"), value: "interval" },
]);

// 表单验证规则
const rules: FormRules = {
  name: [
    {
      required: true,
      message: t("keys.enterGroupName"),
      trigger: ["blur", "input"],
    },
    {
      pattern: /^[a-z0-9_-]{1,100}$/,
      message: t("keys.groupNamePattern"),
      trigger: ["blur", "input"],
    },
  ],
  channel_type: [
    {
      required: true,
      message: t("keys.selectChannelType"),
      trigger: ["blur", "change"],
    },
  ],
  test_model: [
    {
      required: true,
      message: t("keys.enterTestModel"),
      trigger: ["blur", "input"],
    },
  ],
  upstreams: [
    {
      type: "array",
      min: 1,
      message: t("keys.atLeastOneUpstream"),
      trigger: ["blur", "change"],
    },
  ],
};

// 监听弹窗显示状态
watch(
  () => props.show,
  show => {
    if (show) {
      if (!channelTypesFetched.value) {
        fetchChannelTypes();
      }
      if (!configOptionsFetched.value) {
        fetchGroupConfigOptions();
      }
      resetForm();
      if (props.group) {
        loadGroupData();
      }
    }
  }
);

// 监听渠道类型变化，在新增模式下智能更新默认值
watch(
  () => formData.channel_type,
  (_newChannelType, oldChannelType) => {
    if (!props.group && oldChannelType) {
      // 仅在新增模式且不是初始设置时处理
      // 检查测试模型是否应该更新（为空或是旧渠道类型的默认值）
      if (
        !userModifiedFields.value.test_model ||
        formData.test_model === getOldDefaultTestModel(oldChannelType)
      ) {
        formData.test_model = testModelPlaceholder.value;
        userModifiedFields.value.test_model = false;
      }

      // 检查第一个上游地址是否应该更新
      if (
        formData.upstreams.length > 0 &&
        (!userModifiedFields.value.upstream ||
          formData.upstreams[0].url === getOldDefaultUpstream(oldChannelType))
      ) {
        formData.upstreams[0].url = upstreamPlaceholder.value;
        userModifiedFields.value.upstream = false;
      }
    }
  }
);

// 获取旧渠道类型的默认值（用于比较）
function getOldDefaultTestModel(channelType: string): string {
  switch (channelType) {
    case "openai":
    case "openai-response":
      return "gpt-4.1-nano";
    case "gemini":
      return "gemini-2.0-flash-lite";
    case "anthropic":
      return "claude-3-haiku-20240307";
    default:
      return "";
  }
}

function getOldDefaultUpstream(channelType: string): string {
  switch (channelType) {
    case "openai":
    case "openai-response":
      return "https://api.openai.com";
    case "gemini":
      return "https://generativelanguage.googleapis.com";
    case "anthropic":
      return "https://api.anthropic.com";
    default:
      return "";
  }
}

// 重置表单
function resetForm() {
  const isCreateMode = !props.group;
  const defaultChannelType = "openai";

  // 先设置渠道类型，这样 computed 属性能正确计算默认值
  formData.channel_type = defaultChannelType;

  Object.assign(formData, {
    name: "",
    display_name: "",
    description: "",
    upstreams: [
      {
        url: isCreateMode ? upstreamPlaceholder.value : "",
        weight: 1,
      },
    ],
    channel_type: defaultChannelType,
    sort: 1,
    test_model: isCreateMode ? testModelPlaceholder.value : "",
    validation_endpoint: "",
    param_overrides: "",
    model_redirect_rules: "",
    model_redirect_strict: false,
    config: {},
    configItems: [],
    model_rate_limits: [],
    key_request_limit: createRequestLimitDefaults(),
    proxy_pool_items: [],
    proxy_pool_new_url: "",
    proxy_pool_cooldown_seconds: 60,
    proxy_pool_auto_enable_interval_seconds: 60,
    header_rules: [],
    proxy_keys: "",
    group_type: "standard",
  });

  // 重置用户修改状态追踪
  if (isCreateMode) {
    userModifiedFields.value = {
      test_model: false,
      upstream: false,
    };
  }
}

// 加载分组数据（编辑模式）
function loadGroupData() {
  if (!props.group) {
    return;
  }

  const rawConfig = props.group.config || {};
  const {
    model_rate_limits: modelRateLimits,
    key_request_limit: keyRequestLimit,
    proxy_pool: proxyPool,
    ...standardConfig
  } = rawConfig;
  const configItems = Object.entries(standardConfig || {}).map(([key, value]) => {
    return {
      key,
      value: value as number | string | boolean,
    };
  });

  const normalizedKeyRequestLimit = normalizeKeyRequestLimit(keyRequestLimit);
  const normalizedProxyPool = normalizeProxyPool(proxyPool);
  Object.assign(formData, {
    name: props.group.name || "",
    display_name: props.group.display_name || "",
    description: props.group.description || "",
    upstreams: props.group.upstreams?.length
      ? [...props.group.upstreams]
      : [{ url: "", weight: 1 }],
    channel_type: props.group.channel_type || "openai",
    sort: props.group.sort || 1,
    test_model: props.group.test_model || "",
    validation_endpoint: props.group.validation_endpoint || "",
    param_overrides: JSON.stringify(props.group.param_overrides || {}, null, 2),
    model_redirect_rules: JSON.stringify(props.group.model_redirect_rules || {}, null, 2),
    model_redirect_strict: props.group.model_redirect_strict || false,
    config: {},
    configItems,
    model_rate_limits: normalizeModelRateLimits(modelRateLimits),
    key_request_limit: normalizedKeyRequestLimit,
    proxy_pool_items: normalizedProxyPool.items,
    proxy_pool_new_url: "",
    proxy_pool_cooldown_seconds: normalizedProxyPool.cooldown_seconds,
    proxy_pool_auto_enable_interval_seconds: normalizedProxyPool.auto_enable_interval_seconds,
    header_rules: (props.group.header_rules || []).map((rule: HeaderRuleItem) => ({
      key: rule.key || "",
      value: rule.value || "",
      action: (rule.action as "set" | "remove") || "set",
    })),
    proxy_keys: props.group.proxy_keys || "",
    group_type: props.group.group_type || "standard",
  });
}

async function fetchChannelTypes() {
  const options = (await settingsApi.getChannelTypes()) || [];
  channelTypeOptions.value =
    options?.map((type: string) => ({
      label: type,
      value: type,
    })) || [];
  channelTypesFetched.value = true;
}

// 添加上游地址
function addUpstream() {
  formData.upstreams.push({
    url: "",
    weight: 1,
  });
}

// 删除上游地址
function removeUpstream(index: number) {
  if (formData.upstreams.length > 1) {
    formData.upstreams.splice(index, 1);
  } else {
    message.warning(t("keys.atLeastOneUpstream"));
  }
}

async function fetchGroupConfigOptions() {
  const options = await keysApi.getGroupConfigOptions();
  configOptions.value = options || [];
  configOptionsFetched.value = true;
}

// 添加配置项
function addConfigItem() {
  formData.configItems.push({
    key: "",
    value: "",
  });
}

// 删除配置项
function removeConfigItem(index: number) {
  formData.configItems.splice(index, 1);
}

function normalizeModelRateLimits(value: unknown): ModelRateLimitItem[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .filter(item => item && typeof item === "object")
    .map(item => {
      const record = item as Record<string, unknown>;
      return {
        model: String(record.model || ""),
        rpm: typeof record.rpm === "number" ? record.rpm : null,
        tpm: typeof record.tpm === "number" ? record.tpm : null,
        request_limit: normalizeRequestLimit(record.request_limit),
      };
    });
}

function normalizeRequestLimit(value: unknown): RequestLimitForm {
  const defaults = createRequestLimitDefaults();
  if (!value || typeof value !== "object") {
    return defaults;
  }
  const record = value as Record<string, unknown>;
  const resetMode = record.reset_mode === "interval" ? "interval" : "daily";
  return {
    max_requests: typeof record.max_requests === "number" ? record.max_requests : null,
    reset_mode: resetMode,
    interval_minutes:
      typeof record.interval_minutes === "number"
        ? record.interval_minutes
        : defaults.interval_minutes,
    reset_time: typeof record.reset_time === "string" ? record.reset_time : defaults.reset_time,
  };
}

function normalizeKeyRequestLimit(value: unknown): KeyRequestLimitForm {
  return normalizeRequestLimit(value);
}

function createProxyPoolItem(url = ""): ProxyPoolItemForm {
  return {
    url,
    disabled: false,
    permanent_disabled: false,
    notes: "",
    last_test_ok: null,
    last_test_error: "",
    last_test_at: "",
    testing: false,
  };
}

function normalizeProxyPool(value: unknown): {
  items: ProxyPoolItemForm[];
  cooldown_seconds: number;
  auto_enable_interval_seconds: number;
} {
  const defaults = {
    items: [] as ProxyPoolItemForm[],
    cooldown_seconds: 60,
    auto_enable_interval_seconds: 60,
  };
  if (!value) {
    return defaults;
  }
  if (typeof value === "string") {
    return { ...defaults, items: splitProxyText(value).map(url => createProxyPoolItem(url)) };
  }
  if (Array.isArray(value)) {
    return {
      ...defaults,
      items: value.map(item => createProxyPoolItem(String(item))).filter(item => item.url),
    };
  }
  if (typeof value === "object") {
    const record = value as Record<string, unknown>;
    let items: ProxyPoolItemForm[] = [];
    if (Array.isArray(record.items)) {
      items = record.items
        .map(item => normalizeProxyPoolItem(item))
        .filter((item): item is ProxyPoolItemForm => !!item);
    }
    const legacyItems = Array.isArray(record.proxies)
      ? record.proxies.map(item => createProxyPoolItem(String(item))).filter(item => item.url)
      : [];
    const byURL = new Map<string, ProxyPoolItemForm>();
    for (const item of [...items, ...legacyItems]) {
      if (!byURL.has(item.url)) {
        byURL.set(item.url, item);
      }
    }
    const cooldownSeconds =
      typeof record.cooldown_seconds === "number"
        ? record.cooldown_seconds
        : defaults.cooldown_seconds;
    return {
      items: Array.from(byURL.values()),
      cooldown_seconds: cooldownSeconds,
      auto_enable_interval_seconds:
        typeof record.auto_enable_interval_seconds === "number"
          ? record.auto_enable_interval_seconds
          : cooldownSeconds,
    };
  }
  return defaults;
}

function normalizeProxyPoolItem(value: unknown): ProxyPoolItemForm | null {
  if (typeof value === "string") {
    return createProxyPoolItem(value);
  }
  if (!value || typeof value !== "object") {
    return null;
  }
  const record = value as Record<string, unknown>;
  const url = typeof record.url === "string" ? record.url.trim() : "";
  if (!url) {
    return null;
  }
  return {
    url,
    disabled: record.disabled === true,
    permanent_disabled: record.permanent_disabled === true,
    notes: typeof record.notes === "string" ? record.notes : "",
    last_test_ok: typeof record.last_test_ok === "boolean" ? record.last_test_ok : null,
    last_test_error: typeof record.last_test_error === "string" ? record.last_test_error : "",
    last_test_at: typeof record.last_test_at === "string" ? record.last_test_at : "",
    testing: false,
  };
}

function splitProxyText(value: string): string[] {
  return value
    .split(/[\n\r,]+/)
    .map(item => item.trim())
    .filter(Boolean);
}

function addProxyPoolItem() {
  const urls = splitProxyText(formData.proxy_pool_new_url);
  if (urls.length === 0) {
    return;
  }
  const existing = new Set(formData.proxy_pool_items.map(item => item.url));
  for (const url of urls) {
    if (existing.has(url)) {
      continue;
    }
    formData.proxy_pool_items.push(createProxyPoolItem(url));
    existing.add(url);
  }
  formData.proxy_pool_new_url = "";
}

function removeProxyPoolItem(index: number) {
  formData.proxy_pool_items.splice(index, 1);
}

function proxyPoolTargetURL(): string {
  const firstUpstream = formData.upstreams.find(upstream => upstream.url.trim());
  return firstUpstream?.url.trim() || upstreamPlaceholder.value;
}

async function testProxyPoolItem(item: ProxyPoolItemForm) {
  if (!item.url.trim()) {
    message.error(t("keys.proxyUrlRequired"));
    return;
  }

  item.testing = true;
  try {
    const result = await keysApi.testProxy(item.url.trim(), proxyPoolTargetURL());
    item.last_test_ok = result.ok;
    item.last_test_error = result.error || "";
    item.last_test_at = result.checked_at;
    if (result.ok) {
      message.success(t("keys.proxyTestSuccess", { duration: `${result.duration_ms}ms` }));
    } else {
      message.error(result.error || t("keys.proxyTestFailed"));
    }
  } catch (_error) {
    item.last_test_ok = false;
    item.last_test_error = t("keys.proxyTestFailed");
    item.last_test_at = new Date().toISOString();
  } finally {
    item.testing = false;
  }
}

function buildProxyPoolPayload(): Record<string, unknown> | null {
  const items = formData.proxy_pool_items
    .map(item => ({
      url: item.url.trim(),
      disabled: item.disabled,
      permanent_disabled: item.permanent_disabled,
      notes: item.notes.trim(),
      last_test_ok: item.last_test_ok,
      last_test_error: item.last_test_error,
      last_test_at: item.last_test_at,
    }))
    .filter(item => item.url);

  if (items.length === 0) {
    return null;
  }

  return {
    proxies: items.map(item => item.url),
    items,
    cooldown_seconds: formData.proxy_pool_cooldown_seconds || 60,
    auto_enable_interval_seconds: formData.proxy_pool_auto_enable_interval_seconds || 60,
  };
}

function normalizeDailyResetTime(value: string): string | null {
  const parts = value.trim().split(":");
  if (parts.length !== 2 && parts.length !== 3) {
    return null;
  }
  if (!/^\d{1,2}$/.test(parts[0])) {
    return null;
  }
  const hour = Number(parts[0]);
  const minute = Number(parts[1]);
  if (!Number.isInteger(hour) || hour < 0 || hour > 23) {
    return null;
  }
  if (!/^\d{2}$/.test(parts[1]) || !Number.isInteger(minute) || minute < 0 || minute > 59) {
    return null;
  }
  if (parts.length === 2) {
    return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
  }
  const second = Number(parts[2]);
  if (!/^\d{2}$/.test(parts[2]) || !Number.isInteger(second) || second < 0 || second > 59) {
    return null;
  }
  return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}:${String(second).padStart(2, "0")}`;
}

function buildRequestLimitPayload(limit: RequestLimitForm): Record<string, unknown> | null | false {
  if (!limit.max_requests || limit.max_requests <= 0) {
    return null;
  }

  const payload: Record<string, unknown> = {
    max_requests: limit.max_requests,
    reset_mode: limit.reset_mode,
  };
  if (limit.reset_mode === "interval") {
    if (!limit.interval_minutes || limit.interval_minutes <= 0) {
      return false;
    }
    payload.interval_minutes = limit.interval_minutes;
    return payload;
  }

  const resetTime = normalizeDailyResetTime(limit.reset_time);
  if (!resetTime) {
    return false;
  }
  payload.reset_time = resetTime;
  return payload;
}

function addModelRateLimit() {
  formData.model_rate_limits.push({
    model: "",
    rpm: null,
    tpm: null,
    request_limit: createRequestLimitDefaults(),
  });
}

function removeModelRateLimit(index: number) {
  formData.model_rate_limits.splice(index, 1);
}

// 添加Header规则
function addHeaderRule() {
  formData.header_rules.push({
    key: "",
    value: "",
    action: "set",
  });
}

// 删除Header规则
function removeHeaderRule(index: number) {
  formData.header_rules.splice(index, 1);
}

// 规范化Header Key到Canonical格式（模拟HTTP标准）
function canonicalHeaderKey(key: string): string {
  if (!key) {
    return key;
  }
  return key
    .split("-")
    .map(part => part.charAt(0).toUpperCase() + part.slice(1).toLowerCase())
    .join("-");
}

// 验证Header Key唯一性（使用Canonical格式对比）
function validateHeaderKeyUniqueness(
  rules: HeaderRuleItem[],
  currentIndex: number,
  key: string
): boolean {
  if (!key.trim()) {
    return true;
  }

  const canonicalKey = canonicalHeaderKey(key.trim());
  return !rules.some(
    (rule, index) => index !== currentIndex && canonicalHeaderKey(rule.key.trim()) === canonicalKey
  );
}

// 当配置项的key改变时，设置默认值
function handleConfigKeyChange(index: number, key: string) {
  const option = configOptions.value.find(opt => opt.key === key);
  if (option) {
    formData.configItems[index].value = option.default_value;
  }
}

const getConfigOption = (key: string) => {
  return configOptions.value.find(opt => opt.key === key);
};

// 关闭弹窗
function handleClose() {
  emit("update:show", false);
}

// 提交表单
async function handleSubmit() {
  if (loading.value) {
    return;
  }

  try {
    await formRef.value?.validate();

    loading.value = true;

    // 验证 JSON 格式
    let paramOverrides = {};
    if (formData.param_overrides) {
      try {
        paramOverrides = JSON.parse(formData.param_overrides);
      } catch {
        message.error(t("keys.invalidJsonFormat"));
        return;
      }
    }

    // 验证模型重定向规则 JSON 格式
    let modelRedirectRules = {};
    if (formData.model_redirect_rules) {
      try {
        modelRedirectRules = JSON.parse(formData.model_redirect_rules);

        // Validate rule format
        for (const [key, value] of Object.entries(modelRedirectRules)) {
          if (typeof key !== "string" || typeof value !== "string") {
            message.error(t("keys.modelRedirectInvalidFormat"));
            return;
          }
          if (key.trim() === "" || (value as string).trim() === "") {
            message.error(t("keys.modelRedirectEmptyModel"));
            return;
          }
        }
      } catch {
        message.error(t("keys.modelRedirectInvalidJson"));
        return;
      }
    }

    // 将configItems转换为config对象
    const config: Record<string, unknown> = {};
    formData.configItems.forEach((item: ConfigItem) => {
      if (item.key && item.key.trim()) {
        const option = configOptions.value.find(opt => opt.key === item.key);
        if (option && typeof option.default_value === "number" && typeof item.value === "string") {
          const numValue = Number(item.value);
          config[item.key] = isNaN(numValue) ? 0 : numValue;
        } else {
          config[item.key] = item.value;
        }
      }
    });

    const modelRateLimits: Record<string, unknown>[] = [];
    for (const item of formData.model_rate_limits) {
      const model = item.model.trim();
      const rpm = Number(item.rpm || 0);
      const tpm = Number(item.tpm || 0);
      const requestLimit = buildRequestLimitPayload(item.request_limit);
      if (requestLimit === false) {
        message.error(t("keys.modelRateLimitInvalid"));
        return;
      }

      const hasModelLimit = rpm > 0 || tpm > 0 || !!requestLimit;
      if (!model && !hasModelLimit) {
        continue;
      }
      if (!model || !hasModelLimit) {
        message.error(t("keys.modelRateLimitInvalid"));
        return;
      }

      const payload: Record<string, unknown> = { model };
      if (rpm > 0) {
        payload.rpm = rpm;
      }
      if (tpm > 0) {
        payload.tpm = tpm;
      }
      if (requestLimit) {
        payload.request_limit = requestLimit;
      }
      modelRateLimits.push(payload);
    }
    if (modelRateLimits.length > 0) {
      config.model_rate_limits = modelRateLimits;
    }

    const keyRequestLimit = buildRequestLimitPayload(formData.key_request_limit);
    if (keyRequestLimit === false) {
      message.error(t("keys.keyRequestLimitInvalid"));
      return;
    }
    if (keyRequestLimit) {
      config.key_request_limit = keyRequestLimit;
    }

    const proxyPool = buildProxyPoolPayload();
    if (proxyPool) {
      config.proxy_pool = proxyPool;
    }

    // 构建提交数据
    const submitData = {
      name: formData.name,
      display_name: formData.display_name,
      description: formData.description,
      upstreams: formData.upstreams.filter((upstream: UpstreamInfo) => upstream.url.trim()),
      channel_type: formData.channel_type,
      sort: formData.sort,
      test_model: formData.test_model,
      validation_endpoint: formData.validation_endpoint,
      param_overrides: paramOverrides,
      model_redirect_rules: modelRedirectRules,
      model_redirect_strict: formData.model_redirect_strict,
      config,
      header_rules: formData.header_rules
        .filter((rule: HeaderRuleItem) => rule.key.trim())
        .map((rule: HeaderRuleItem) => ({
          key: rule.key.trim(),
          value: rule.value,
          action: rule.action,
        })),
      proxy_keys: formData.proxy_keys,
    };

    let res: Group;
    if (props.group?.id) {
      // 编辑模式
      res = await keysApi.updateGroup(props.group.id, submitData);
    } else {
      // 新建模式
      res = await keysApi.createGroup(submitData);
    }

    emit("success", res);
    // 如果是新建模式，发出切换到新分组的事件
    if (!props.group?.id && res.id) {
      emit("switchToGroup", res.id);
    }
    handleClose();
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <n-modal :show="show" @update:show="handleClose" class="group-form-modal">
    <n-card
      class="group-form-card"
      :title="group ? t('keys.editGroup') : t('keys.createGroup')"
      :bordered="false"
      size="huge"
      role="dialog"
      aria-modal="true"
    >
      <template #header-extra>
        <n-button quaternary circle @click="handleClose">
          <template #icon>
            <n-icon :component="Close" />
          </template>
        </n-button>
      </template>

      <n-form
        ref="formRef"
        :model="formData"
        :rules="rules"
        label-placement="left"
        label-width="120px"
        require-mark-placement="right-hanging"
        class="group-form"
      >
        <!-- 基础信息 -->
        <div class="form-section">
          <h4 class="section-title">{{ t("keys.basicInfo") }}</h4>

          <!-- Group name and display name on the same row -->
          <div class="form-row">
            <n-form-item :label="t('keys.groupName')" path="name" class="form-item-half">
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.groupName") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.groupNameTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-input v-model:value="formData.name" placeholder="gemini" />
            </n-form-item>

            <n-form-item :label="t('keys.displayName')" path="display_name" class="form-item-half">
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.displayName") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.displayNameTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-input v-model:value="formData.display_name" placeholder="Google Gemini" />
            </n-form-item>
          </div>

          <!-- Channel type and sort order on the same row -->
          <div class="form-row">
            <n-form-item :label="t('keys.channelType')" path="channel_type" class="form-item-half">
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.channelType") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.channelTypeTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-select
                v-model:value="formData.channel_type"
                :options="channelTypeOptions"
                :placeholder="t('keys.selectChannelType')"
              />
            </n-form-item>

            <n-form-item :label="t('keys.sortOrder')" path="sort" class="form-item-half">
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.sortOrder") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.sortOrderTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-input-number
                v-model:value="formData.sort"
                :min="0"
                :placeholder="t('keys.sortValue')"
                style="width: 100%"
              />
            </n-form-item>
          </div>

          <!-- Test model and test path on the same row -->
          <div class="form-row">
            <n-form-item :label="t('keys.testModel')" path="test_model" class="form-item-half">
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.testModel") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    {{ t("keys.testModelTooltip") }}
                  </n-tooltip>
                </div>
              </template>
              <n-input
                v-model:value="formData.test_model"
                :placeholder="testModelPlaceholder"
                @input="() => !props.group && (userModifiedFields.test_model = true)"
              />
            </n-form-item>

            <n-form-item
              :label="t('keys.testPath')"
              path="validation_endpoint"
              class="form-item-half"
              v-if="formData.channel_type !== 'gemini'"
            >
              <template #label>
                <div class="form-label-with-tooltip">
                  {{ t("keys.testPath") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon" />
                    </template>
                    <div>
                      {{ t("keys.testPathTooltip1") }}
                      <br />
                      • OpenAI: /v1/chat/completions
                      <br />
                      • OpenAI Response: /v1/responses
                      <br />
                      • Anthropic: /v1/messages
                      <br />
                      {{ t("keys.testPathTooltip2") }}
                    </div>
                  </n-tooltip>
                </div>
              </template>
              <n-input
                v-model:value="formData.validation_endpoint"
                :placeholder="
                  validationEndpointPlaceholder || t('keys.optionalCustomValidationPath')
                "
              />
            </n-form-item>

            <!-- When gemini channel, test path is hidden, need placeholder div to keep layout -->
            <div v-else class="form-item-half" />
          </div>

          <!-- Proxy keys -->
          <n-form-item :label="t('keys.proxyKeys')" path="proxy_keys">
            <template #label>
              <div class="form-label-with-tooltip">
                {{ t("keys.proxyKeys") }}
                <n-tooltip trigger="hover" placement="top">
                  <template #trigger>
                    <n-icon :component="HelpCircleOutline" class="help-icon" />
                  </template>
                  {{ t("keys.proxyKeysTooltip") }}
                </n-tooltip>
              </div>
            </template>
            <proxy-keys-input
              v-model="formData.proxy_keys"
              :placeholder="t('keys.multiKeysPlaceholder')"
              size="medium"
            />
          </n-form-item>

          <!-- Description takes full row -->
          <n-form-item :label="t('common.description')" path="description">
            <template #label>
              <div class="form-label-with-tooltip">
                {{ t("common.description") }}
                <n-tooltip trigger="hover" placement="top">
                  <template #trigger>
                    <n-icon :component="HelpCircleOutline" class="help-icon" />
                  </template>
                  {{ t("keys.descriptionTooltip") }}
                </n-tooltip>
              </div>
            </template>
            <n-input
              v-model:value="formData.description"
              type="textarea"
              placeholder=""
              :rows="1"
              :autosize="{ minRows: 1, maxRows: 5 }"
              style="resize: none"
            />
          </n-form-item>
        </div>

        <!-- Upstream addresses -->
        <div class="form-section" style="margin-top: 10px">
          <h4 class="section-title">{{ t("keys.upstreamAddresses") }}</h4>
          <n-form-item
            v-for="(upstream, index) in formData.upstreams"
            :key="index"
            :label="`${t('keys.upstream')} ${index + 1}`"
            :path="`upstreams[${index}].url`"
            :rule="{
              required: true,
              message: '',
              trigger: ['blur', 'input'],
            }"
          >
            <template #label>
              <div class="form-label-with-tooltip">
                {{ t("keys.upstream") }} {{ index + 1 }}
                <n-tooltip trigger="hover" placement="top">
                  <template #trigger>
                    <n-icon :component="HelpCircleOutline" class="help-icon" />
                  </template>
                  {{ t("keys.upstreamTooltip") }}
                </n-tooltip>
              </div>
            </template>
            <div class="upstream-row">
              <div class="upstream-url">
                <n-input
                  v-model:value="upstream.url"
                  :placeholder="upstreamPlaceholder"
                  @input="() => !props.group && index === 0 && (userModifiedFields.upstream = true)"
                />
              </div>
              <div class="upstream-weight">
                <span class="weight-label">{{ t("keys.weight") }}</span>
                <n-tooltip trigger="hover" placement="top" style="width: 100%">
                  <template #trigger>
                    <n-input-number
                      v-model:value="upstream.weight"
                      :min="0"
                      :placeholder="t('keys.weight')"
                      style="width: 100%"
                    />
                  </template>
                  {{ t("keys.weightTooltip") }}
                </n-tooltip>
              </div>
              <div class="upstream-actions">
                <n-button
                  v-if="formData.upstreams.length > 1"
                  @click="removeUpstream(index)"
                  type="error"
                  quaternary
                  circle
                  size="small"
                >
                  <template #icon>
                    <n-icon :component="Remove" />
                  </template>
                </n-button>
              </div>
            </div>
          </n-form-item>

          <n-form-item>
            <n-button @click="addUpstream" dashed style="width: 100%">
              <template #icon>
                <n-icon :component="Add" />
              </template>
              {{ t("keys.addUpstream") }}
            </n-button>
          </n-form-item>
        </div>

        <!-- Advanced configuration -->
        <div class="form-section" style="margin-top: 10px">
          <n-collapse>
            <n-collapse-item name="advanced">
              <template #header>{{ t("keys.advancedConfig") }}</template>
              <div class="config-section">
                <h5 class="config-title-with-tooltip">
                  {{ t("keys.usageLimits") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                    </template>
                    {{ t("keys.usageLimitsTooltip") }}
                  </n-tooltip>
                </h5>

                <div class="rate-limit-items">
                  <n-form-item
                    v-for="(limit, index) in formData.model_rate_limits"
                    :key="index"
                    class="rate-limit-row"
                    :label="`${t('keys.modelLimit')} ${index + 1}`"
                  >
                    <template #label>
                      <div class="form-label-with-tooltip">
                        {{ t("keys.modelLimit") }} {{ index + 1 }}
                        <n-tooltip trigger="hover" placement="top">
                          <template #trigger>
                            <n-icon :component="HelpCircleOutline" class="help-icon" />
                          </template>
                          {{ t("keys.modelLimitTooltip") }}
                        </n-tooltip>
                      </div>
                    </template>
                    <div class="rate-limit-content">
                      <div class="rate-limit-main">
                        <n-input v-model:value="limit.model" placeholder="gpt-4.1, *" />
                        <n-input-number
                          v-model:value="limit.rpm"
                          :min="0"
                          :precision="0"
                          placeholder="RPM"
                          style="width: 100%"
                        />
                        <n-input-number
                          v-model:value="limit.tpm"
                          :min="0"
                          :precision="0"
                          :placeholder="t('keys.tpmPlaceholder')"
                          style="width: 100%"
                        />
                        <n-input-number
                          v-model:value="limit.request_limit.max_requests"
                          :min="0"
                          :precision="0"
                          :placeholder="t('keys.modelMaxRequests')"
                          style="width: 100%"
                        />
                        <div class="config-actions">
                          <n-button
                            @click="removeModelRateLimit(index)"
                            type="error"
                            quaternary
                            circle
                            size="small"
                          >
                            <template #icon>
                              <n-icon :component="Remove" />
                            </template>
                          </n-button>
                        </div>
                      </div>
                      <div class="rate-limit-reset">
                        <n-select
                          v-model:value="limit.request_limit.reset_mode"
                          :options="resetModeOptions"
                        />
                        <n-input-number
                          v-if="limit.request_limit.reset_mode === 'interval'"
                          v-model:value="limit.request_limit.interval_minutes"
                          :min="1"
                          :precision="0"
                          :placeholder="t('keys.intervalMinutes')"
                          style="width: 100%"
                        />
                        <n-input
                          v-else
                          v-model:value="limit.request_limit.reset_time"
                          placeholder="00:00 PT"
                        />
                      </div>
                    </div>
                  </n-form-item>
                </div>

                <div style="margin-top: 12px; padding-left: 120px">
                  <n-button @click="addModelRateLimit" dashed style="width: 100%">
                    <template #icon>
                      <n-icon :component="Add" />
                    </template>
                    {{ t("keys.addModelLimit") }}
                  </n-button>
                </div>

                <n-form-item style="margin-top: 16px" path="key_request_limit">
                  <template #label>
                    <div class="form-label-with-tooltip">
                      {{ t("keys.keyRequestLimit") }}
                      <n-tooltip trigger="hover" placement="top">
                        <template #trigger>
                          <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                        </template>
                        {{ t("keys.keyRequestLimitTooltip") }}
                      </n-tooltip>
                    </div>
                  </template>
                  <div class="key-request-limit-content">
                    <n-input-number
                      v-model:value="formData.key_request_limit.max_requests"
                      :min="0"
                      :precision="0"
                      :placeholder="t('keys.maxRequests')"
                      style="width: 100%"
                    />
                    <n-select
                      v-model:value="formData.key_request_limit.reset_mode"
                      :options="resetModeOptions"
                    />
                    <n-input-number
                      v-if="formData.key_request_limit.reset_mode === 'interval'"
                      v-model:value="formData.key_request_limit.interval_minutes"
                      :min="1"
                      :precision="0"
                      :placeholder="t('keys.intervalMinutes')"
                      style="width: 100%"
                    />
                    <n-input
                      v-else
                      v-model:value="formData.key_request_limit.reset_time"
                      placeholder="00:00 PT"
                    />
                  </div>
                </n-form-item>
              </div>

              <div class="config-section">
                <h5 class="config-title-with-tooltip">
                  {{ t("keys.proxyPool") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                    </template>
                    {{ t("keys.proxyPoolTooltip") }}
                  </n-tooltip>
                </h5>

                <n-form-item path="proxy_pool_new_url">
                  <template #label>
                    <div class="form-label-with-tooltip">
                      {{ t("keys.addProxy") }}
                      <n-tooltip trigger="hover" placement="top">
                        <template #trigger>
                          <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                        </template>
                        {{ t("keys.proxyPoolInputTooltip") }}
                      </n-tooltip>
                    </div>
                  </template>
                  <div class="proxy-pool-add-row">
                    <n-input
                      v-model:value="formData.proxy_pool_new_url"
                      type="textarea"
                      placeholder="http://127.0.0.1:7890"
                      :rows="2"
                    />
                    <n-button type="primary" ghost @click="addProxyPoolItem">
                      <template #icon>
                        <n-icon :component="Add" />
                      </template>
                      {{ t("common.add") }}
                    </n-button>
                  </div>
                </n-form-item>

                <div class="proxy-pool-settings">
                  <n-form-item path="proxy_pool_auto_enable_interval_seconds">
                    <template #label>
                      <div class="form-label-with-tooltip">
                        {{ t("keys.proxyAutoEnableInterval") }}
                        <n-tooltip trigger="hover" placement="top">
                          <template #trigger>
                            <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                          </template>
                          {{ t("keys.proxyAutoEnableIntervalTooltip") }}
                        </n-tooltip>
                      </div>
                    </template>
                    <n-input-number
                      v-model:value="formData.proxy_pool_auto_enable_interval_seconds"
                      :min="0"
                      :precision="0"
                      style="width: 100%"
                    />
                  </n-form-item>
                </div>

                <div class="proxy-pool-list">
                  <div
                    v-for="(item, index) in formData.proxy_pool_items"
                    :key="`${item.url}-${index}`"
                    class="proxy-pool-row"
                  >
                    <div class="proxy-url-cell">
                      <n-input v-model:value="item.url" placeholder="http://127.0.0.1:7890" />
                      <div class="proxy-test-status">
                        <span v-if="item.last_test_ok === true" class="proxy-test-ok">
                          {{ t("keys.proxyTestOk") }}
                        </span>
                        <span v-else-if="item.last_test_ok === false" class="proxy-test-failed">
                          {{ item.last_test_error || t("keys.proxyTestFailed") }}
                        </span>
                        <span v-else>{{ t("keys.proxyUntested") }}</span>
                      </div>
                    </div>
                    <n-input
                      v-model:value="item.notes"
                      class="proxy-notes-cell"
                      :placeholder="t('keys.proxyNotes')"
                    />
                    <div class="proxy-switch-cell">
                      <span>{{ t("keys.proxyDisabled") }}</span>
                      <n-switch v-model:value="item.disabled" />
                    </div>
                    <div class="proxy-switch-cell">
                      <span>{{ t("keys.proxyPermanentDisabled") }}</span>
                      <n-switch v-model:value="item.permanent_disabled" />
                    </div>
                    <div class="proxy-actions-cell">
                      <n-button
                        size="small"
                        type="primary"
                        ghost
                        :loading="item.testing"
                        @click="testProxyPoolItem(item)"
                      >
                        {{ t("keys.testProxy") }}
                      </n-button>
                      <n-button size="small" circle quaternary @click="removeProxyPoolItem(index)">
                        <template #icon>
                          <n-icon :component="Remove" />
                        </template>
                      </n-button>
                    </div>
                  </div>
                </div>
              </div>

              <div class="config-section">
                <h5 class="config-title-with-tooltip">
                  {{ t("keys.groupConfig") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                    </template>
                    {{ t("keys.groupConfigTooltip") }}
                  </n-tooltip>
                </h5>

                <div class="config-items">
                  <n-form-item
                    v-for="(configItem, index) in formData.configItems"
                    :key="index"
                    class="config-item-row"
                    :label="`${t('keys.config')} ${index + 1}`"
                    :path="`configItems[${index}].key`"
                    :rule="{
                      required: true,
                      message: '',
                      trigger: ['blur', 'change'],
                    }"
                  >
                    <template #label>
                      <div class="form-label-with-tooltip">
                        {{ t("keys.config") }} {{ index + 1 }}
                        <n-tooltip trigger="hover" placement="top">
                          <template #trigger>
                            <n-icon :component="HelpCircleOutline" class="help-icon" />
                          </template>
                          {{ t("keys.configTooltip") }}
                        </n-tooltip>
                      </div>
                    </template>
                    <div class="config-item-content">
                      <div class="config-select">
                        <n-select
                          v-model:value="configItem.key"
                          :options="
                            configOptions.map(opt => ({
                              label: opt.name,
                              value: opt.key,
                              disabled:
                                formData.configItems
                                  .map((item: ConfigItem) => item.key)
                                  ?.includes(opt.key) && opt.key !== configItem.key,
                            }))
                          "
                          :placeholder="t('keys.selectConfigParam')"
                          @update:value="value => handleConfigKeyChange(index, value)"
                          clearable
                        />
                      </div>
                      <div class="config-value">
                        <n-tooltip trigger="hover" placement="top">
                          <template #trigger>
                            <n-input-number
                              v-if="typeof configItem.value === 'number'"
                              v-model:value="configItem.value"
                              :placeholder="t('keys.paramValue')"
                              :precision="0"
                              style="width: 100%"
                            />
                            <n-switch
                              v-else-if="typeof configItem.value === 'boolean'"
                              v-model:value="configItem.value"
                              size="small"
                            />
                            <n-input
                              v-else
                              v-model:value="configItem.value"
                              :placeholder="t('keys.paramValue')"
                            />
                          </template>
                          {{
                            getConfigOption(configItem.key)?.description || t("keys.setConfigValue")
                          }}
                        </n-tooltip>
                      </div>
                      <div class="config-actions">
                        <n-button
                          @click="removeConfigItem(index)"
                          type="error"
                          quaternary
                          circle
                          size="small"
                        >
                          <template #icon>
                            <n-icon :component="Remove" />
                          </template>
                        </n-button>
                      </div>
                    </div>
                  </n-form-item>
                </div>

                <div style="margin-top: 12px; padding-left: 120px">
                  <n-button
                    @click="addConfigItem"
                    dashed
                    style="width: 100%"
                    :disabled="formData.configItems.length >= configOptions.length"
                  >
                    <template #icon>
                      <n-icon :component="Add" />
                    </template>
                    {{ t("keys.addConfigParam") }}
                  </n-button>
                </div>
              </div>

              <div class="config-section">
                <h5 class="config-title-with-tooltip">
                  {{ t("keys.customHeaders") }}
                  <n-tooltip trigger="hover" placement="top">
                    <template #trigger>
                      <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                    </template>
                    <div>
                      {{ t("keys.headerRulesTooltip1") }}
                      <br />
                      {{ t("keys.supportedVariables") }}：
                      <br />
                      • ${CLIENT_IP} - {{ t("keys.clientIpVar") }}
                      <br />
                      • ${GROUP_NAME} - {{ t("keys.groupNameVar") }}
                      <br />
                      • ${API_KEY} - {{ t("keys.apiKeyVar") }}
                      <br />
                      • ${TIMESTAMP_MS} - {{ t("keys.timestampMsVar") }}
                      <br />
                      • ${TIMESTAMP_S} - {{ t("keys.timestampSVar") }}
                    </div>
                  </n-tooltip>
                </h5>

                <div class="header-rules-items">
                  <n-form-item
                    v-for="(headerRule, index) in formData.header_rules"
                    :key="index"
                    class="header-rule-row"
                    :label="`${t('keys.header')} ${index + 1}`"
                  >
                    <template #label>
                      <div class="form-label-with-tooltip">
                        {{ t("keys.header") }} {{ index + 1 }}
                        <n-tooltip trigger="hover" placement="top">
                          <template #trigger>
                            <n-icon :component="HelpCircleOutline" class="help-icon" />
                          </template>
                          {{ t("keys.headerTooltip") }}
                        </n-tooltip>
                      </div>
                    </template>
                    <div class="header-rule-content">
                      <div class="header-name">
                        <n-input
                          v-model:value="headerRule.key"
                          :placeholder="t('keys.headerName')"
                          :status="
                            !validateHeaderKeyUniqueness(
                              formData.header_rules,
                              index,
                              headerRule.key
                            )
                              ? 'error'
                              : undefined
                          "
                        />
                        <div
                          v-if="
                            !validateHeaderKeyUniqueness(
                              formData.header_rules,
                              index,
                              headerRule.key
                            )
                          "
                          class="error-message"
                        >
                          {{ t("keys.duplicateHeader") }}
                        </div>
                      </div>
                      <div class="header-value" v-if="headerRule.action === 'set'">
                        <n-input
                          v-model:value="headerRule.value"
                          :placeholder="t('keys.headerValuePlaceholder')"
                        />
                      </div>
                      <div class="header-value removed-placeholder" v-else>
                        <span class="removed-text">{{ t("keys.willRemoveFromRequest") }}</span>
                      </div>
                      <div class="header-action">
                        <n-tooltip trigger="hover" placement="top">
                          <template #trigger>
                            <n-switch
                              v-model:value="headerRule.action"
                              :checked-value="'remove'"
                              :unchecked-value="'set'"
                              size="small"
                            />
                          </template>
                          {{ t("keys.removeToggleTooltip") }}
                        </n-tooltip>
                      </div>
                      <div class="header-actions">
                        <n-button
                          @click="removeHeaderRule(index)"
                          type="error"
                          quaternary
                          circle
                          size="small"
                        >
                          <template #icon>
                            <n-icon :component="Remove" />
                          </template>
                        </n-button>
                      </div>
                    </div>
                  </n-form-item>
                </div>

                <div style="margin-top: 12px; padding-left: 120px">
                  <n-button @click="addHeaderRule" dashed style="width: 100%">
                    <template #icon>
                      <n-icon :component="Add" />
                    </template>
                    {{ t("keys.addHeader") }}
                  </n-button>
                </div>
              </div>

              <!-- 模型重定向配置 -->
              <div v-if="formData.group_type !== 'aggregate'" class="config-section">
                <n-form-item path="model_redirect_strict">
                  <template #label>
                    <div class="form-label-with-tooltip">
                      {{ t("keys.modelRedirectPolicy") }}
                      <n-tooltip trigger="hover" placement="top">
                        <template #trigger>
                          <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                        </template>
                        {{ t("keys.modelRedirectPolicyTooltip") }}
                      </n-tooltip>
                    </div>
                  </template>
                  <div style="display: flex; align-items: center; gap: 12px">
                    <n-switch v-model:value="formData.model_redirect_strict" />
                    <span style="font-size: 14px; color: #666">
                      {{
                        formData.model_redirect_strict
                          ? t("keys.modelRedirectStrictMode")
                          : t("keys.modelRedirectLooseMode")
                      }}
                    </span>
                  </div>
                  <template #feedback>
                    <div style="font-size: 12px; color: #999; margin: 4px 0">
                      <div v-if="formData.model_redirect_strict" style="color: #f5a623">
                        ⚠️ {{ t("keys.modelRedirectStrictWarning") }}
                      </div>
                      <div v-else style="color: #52c41a">
                        ✅ {{ t("keys.modelRedirectLooseInfo") }}
                      </div>
                    </div>
                  </template>
                </n-form-item>

                <n-form-item path="model_redirect_rules">
                  <template #label>
                    <div class="form-label-with-tooltip">
                      {{ t("keys.modelRedirectRules") }}
                      <n-tooltip trigger="hover" placement="top">
                        <template #trigger>
                          <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                        </template>
                        {{ t("keys.modelRedirectRulesTooltip") }}
                      </n-tooltip>
                    </div>
                  </template>
                  <n-input
                    v-model:value="formData.model_redirect_rules"
                    type="textarea"
                    :placeholder="modelRedirectTip"
                    :rows="4"
                  />
                  <template #feedback>
                    <div style="font-size: 14px; color: #999">
                      {{ t("keys.modelRedirectRulesDescription") }}
                    </div>
                  </template>
                </n-form-item>
              </div>

              <div class="config-section">
                <n-form-item path="param_overrides">
                  <template #label>
                    <div class="form-label-with-tooltip">
                      {{ t("keys.paramOverrides") }}
                      <n-tooltip trigger="hover" placement="top">
                        <template #trigger>
                          <n-icon :component="HelpCircleOutline" class="help-icon config-help" />
                        </template>
                        {{ t("keys.paramOverridesTooltip") }}
                      </n-tooltip>
                    </div>
                  </template>
                  <n-input
                    v-model:value="formData.param_overrides"
                    type="textarea"
                    placeholder='{"temperature": 0.7}'
                    :rows="4"
                  />
                </n-form-item>
              </div>
            </n-collapse-item>
          </n-collapse>
        </div>
      </n-form>

      <template #footer>
        <div style="display: flex; justify-content: flex-end; gap: 12px">
          <n-button @click="handleClose">{{ t("common.cancel") }}</n-button>
          <n-button type="primary" @click="handleSubmit" :loading="loading">
            {{ group ? t("common.update") : t("common.create") }}
          </n-button>
        </div>
      </template>
    </n-card>
  </n-modal>
</template>

<style scoped>
.group-form-modal {
  width: 800px;
}

.form-section {
  margin-top: 20px;
}

.section-title {
  font-size: 1rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 16px 0;
  padding-bottom: 8px;
  border-bottom: 2px solid var(--border-color);
}

:deep(.n-form-item-label) {
  font-weight: 500;
}

:deep(.n-form-item-blank) {
  flex-grow: 1;
}

:deep(.n-input) {
  --n-border-radius: 6px;
}

:deep(.n-select) {
  --n-border-radius: 6px;
}

:deep(.n-input-number) {
  --n-border-radius: 6px;
}

:deep(.n-card-header) {
  border-bottom: 1px solid var(--border-color);
  padding: 10px 20px;
}

:deep(.n-card__content) {
  max-height: calc(100vh - 68px - 61px - 50px);
  overflow-y: auto;
}

:deep(.n-card__footer) {
  border-top: 1px solid var(--border-color);
  padding: 10px 15px;
}

:deep(.n-form-item-feedback-wrapper) {
  min-height: 10px;
}

.config-section {
  margin-top: 16px;
}

.config-title {
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 12px 0;
}

.form-label {
  margin-left: 25px;
  margin-right: 10px;
  height: 34px;
  line-height: 34px;
  font-weight: 500;
}

/* Tooltip相关样式 */
.form-label-with-tooltip {
  display: flex;
  align-items: center;
  gap: 6px;
}

.help-icon {
  color: var(--text-tertiary);
  font-size: 14px;
  cursor: help;
  transition: color 0.2s ease;
}

.help-icon:hover {
  color: var(--primary-color);
}

.section-title-with-tooltip {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
}

.section-help {
  font-size: 16px;
}

.collapse-header-with-tooltip {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
}

.collapse-help {
  font-size: 14px;
}

.config-title-with-tooltip {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 12px 0;
}

.config-help {
  font-size: 13px;
}

/* 增强表单样式 */
:deep(.n-form-item-label) {
  font-weight: 500;
  color: var(--text-primary);
}

:deep(.n-input) {
  --n-border-radius: 8px;
  --n-border: 1px solid var(--border-color);
  --n-border-hover: 1px solid var(--primary-color);
  --n-border-focus: 1px solid var(--primary-color);
  --n-box-shadow-focus: 0 0 0 2px var(--primary-color-suppl);
}

:deep(.n-select) {
  --n-border-radius: 8px;
}

:deep(.n-input-number) {
  --n-border-radius: 8px;
}

:deep(.n-button) {
  --n-border-radius: 8px;
}

/* 美化tooltip */
:deep(.n-tooltip__trigger) {
  display: inline-flex;
  align-items: center;
}

:deep(.n-tooltip) {
  --n-font-size: 13px;
  --n-border-radius: 8px;
}

:deep(.n-tooltip .n-tooltip__content) {
  max-width: 320px;
  line-height: 1.5;
}

:deep(.n-tooltip .n-tooltip__content div) {
  white-space: pre-line;
}

/* 折叠面板样式优化 */
:deep(.n-collapse-item__header) {
  font-weight: 500;
  color: var(--text-primary);
}

:deep(.n-collapse-item) {
  --n-title-padding: 16px 0;
}

:deep(.n-base-selection-label) {
  height: 40px;
}

/* 表单行布局 */
.form-row {
  display: flex;
  gap: 20px;
  align-items: flex-start;
}

.form-item-half {
  flex: 1;
  width: 50%;
}

/* 上游地址行布局 */
.upstream-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
}

.upstream-url {
  flex: 1;
}

.upstream-weight {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 140px;
}

.weight-label {
  font-weight: 500;
  color: var(--text-primary);
  white-space: nowrap;
}

.upstream-actions {
  flex: 0 0 32px;
  display: flex;
  justify-content: center;
}

/* 配置项行布局 */
.config-item-row {
  margin-bottom: 12px;
}

.config-item-content {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
}

.config-select {
  flex: 0 0 200px;
}

.config-value {
  flex: 1;
}

.config-actions {
  flex: 0 0 32px;
  display: flex;
  justify-content: center;
}

.rate-limit-row {
  margin-bottom: 12px;
}

.rate-limit-content {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: 100%;
}

.rate-limit-main {
  display: grid;
  grid-template-columns: minmax(160px, 1fr) 110px 150px 150px 32px;
  gap: 12px;
  width: 100%;
  align-items: center;
}

.rate-limit-reset {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  gap: 12px;
  width: 100%;
}

.key-request-limit-content {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 160px minmax(0, 1fr);
  gap: 12px;
  width: 100%;
}

.proxy-pool-add-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 110px;
  gap: 12px;
  width: 100%;
  align-items: flex-start;
}

.proxy-pool-settings {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
}

.proxy-pool-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.proxy-pool-row {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(120px, 180px) 96px 120px 150px;
  gap: 10px;
  align-items: center;
  padding: 10px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
}

.proxy-url-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.proxy-notes-cell {
  min-width: 0;
}

.proxy-test-status {
  min-height: 18px;
  font-size: 12px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}

.proxy-test-ok {
  color: var(--success-color);
}

.proxy-test-failed {
  color: var(--error-color);
}

.proxy-switch-cell {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
  color: var(--text-secondary);
}

.proxy-actions-cell {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

@media (max-width: 768px) {
  .group-form-card {
    width: 100vw !important;
  }

  .group-form {
    width: auto !important;
  }

  .form-row {
    flex-direction: column;
    gap: 0;
  }

  .form-item-half {
    width: 100%;
  }

  .section-title {
    font-size: 0.9rem;
  }

  .upstream-row,
  .config-item-content {
    flex-direction: column;
    gap: 8px;
    align-items: stretch;
  }

  .upstream-weight {
    flex: 1;
    flex-direction: column;
    align-items: flex-start;
  }

  .config-value {
    flex: 1;
  }

  .rate-limit-main,
  .rate-limit-reset {
    grid-template-columns: 1fr;
  }

  .key-request-limit-content {
    grid-template-columns: 1fr;
  }

  .proxy-pool-add-row,
  .proxy-pool-row {
    grid-template-columns: 1fr;
  }

  .proxy-actions-cell {
    justify-content: flex-start;
  }

  .upstream-actions,
  .config-actions {
    justify-content: flex-end;
  }
}

/* Header规则相关样式 */
.header-rule-row {
  margin-bottom: 12px;
}

.header-rule-content {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  width: 100%;
}

.header-name {
  flex: 0 0 200px;
  position: relative;
}

.header-value {
  flex: 1;
  display: flex;
  align-items: center;
  min-height: 34px;
}

.header-value.removed-placeholder {
  justify-content: center;
  background-color: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 0 12px;
}

.removed-text {
  color: var(--text-tertiary);
  font-style: italic;
  font-size: 13px;
}

.header-action {
  flex: 0 0 50px;
  display: flex;
  justify-content: center;
  align-items: center;
  height: 34px;
}

.header-actions {
  flex: 0 0 32px;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  height: 34px;
}

.error-message {
  position: absolute;
  top: 100%;
  left: 0;
  font-size: 12px;
  color: var(--error-color);
  margin-top: 2px;
}

@media (max-width: 768px) {
  .header-rule-content {
    flex-direction: column;
    gap: 8px;
    align-items: stretch;
  }

  .header-name,
  .header-value {
    flex: 1;
  }

  .header-actions {
    justify-content: flex-end;
  }
}
</style>
