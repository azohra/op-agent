local cmd = require("cmd")
local json = require("json")

local function valid_env_name(value)
    return type(value) == "string" and value:match("^[%a_][%w_]*$") ~= nil
end

local function valid_profile(value)
    return type(value) == "string"
        and #value <= 32
        and value:match("^[a-z][a-z0-9-]*$") ~= nil
end

local function normalize_keys(value)
    local keys = {}
    if type(value) == "string" then
        for key in value:gmatch("[^,%s]+") do
            table.insert(keys, key)
        end
    elseif type(value) == "table" then
        for _, key in ipairs(value) do
            table.insert(keys, key)
        end
    else
        error("op-agent: keys must be a string or array")
    end

    if #keys == 0 then
        error("op-agent: keys must contain at least one environment name")
    end
    for _, key in ipairs(keys) do
        if not valid_env_name(key) then
            error("op-agent: invalid environment name in keys")
        end
    end
    return table.concat(keys, ",")
end

function PLUGIN:MiseEnv(ctx)
    local options = ctx.options or {}
    local allowed_options = {
        account = true,
        fresh = true,
        keys = true,
        profile = true,
    }
    for name, _ in pairs(options) do
        if not allowed_options[name] then
            error("op-agent: unsupported option " .. tostring(name))
        end
    end

    local profile = options.profile
    local account = options.account
    if profile == nil then
        profile = ""
    end
    if account == nil then
        account = "default"
    end

    if profile ~= "" and not valid_profile(profile) then
        error("op-agent: profile must contain 1-32 lowercase letters, numbers, or hyphens and start with a letter")
    end
    if type(account) ~= "string" or account:match("^[%w][%w._-]*$") == nil or #account > 64 then
        error("op-agent: invalid account")
    end

    local child_env = {
        OP_AGENT_KEYS = normalize_keys(options.keys),
        OP_AGENT_PROFILE = profile,
        OP_AGENT_ACCOUNT = account,
    }
    if options.fresh == true then
        child_env.OP_AGENT_FRESH = "true"
    elseif options.fresh ~= nil and options.fresh ~= false then
        error("op-agent: fresh must be a boolean")
    end

    local decoded = json.decode(cmd.exec("op-agent env --format json", { env = child_env }))
    if type(decoded) ~= "table" then
        error("op-agent: env command returned invalid JSON")
    end

    local names = {}
    for name, value in pairs(decoded) do
        if not valid_env_name(name) or type(value) ~= "string" then
            error("op-agent: env command returned an invalid environment entry")
        end
        table.insert(names, name)
    end
    table.sort(names)

    local env = {}
    for _, name in ipairs(names) do
        table.insert(env, { key = name, value = decoded[name] })
    end
    return {
        env = env,
        cacheable = false,
        redact = true,
    }
end
