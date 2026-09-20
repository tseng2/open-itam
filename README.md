# Open-ITAM 开源企业级资产管理系统

> 自研跨平台终端自动化数据采集 Agent + 企业级 IT 资产全生命周期管理系统 (ITAM)

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/tseng2/open-itam)](https://goreportcard.com/report/github.com/tseng2/open-itam)

## 📌 项目定位升级

本项目已从早期的“单一终端采集器”全面升级为自研企业级 IT 资产管理中枢（**Open-ITAM**），涵盖：
- **服务端 (Server)**：多组织/多公司管控隔离、硬件固定资产台账、组织与人员架构 (AD 同步)、财务与采购订单 (用友 U8) 关联、RBAC 角色权限体系。
- **采集端 (Agent)**：轻量化常驻服务（Windows / Linux / macOS），自动化采集 CPU、内存、硬盘 SMART 健康度、网卡、主板序列号及已装软件列表，硬件变更实时预警。

---

## 🌟 核心特性

- 🏢 **集团化多公司隔离**：支持多租户与分公司权限划分，数据物理/逻辑强隔离。
- 🖥️ **软硬件指纹画像**：Agent 自动探测上报底层硬件参数、SMART 健康寿命及商业软件列表。
- 🔐 **内置 RBAC 权限体系**：支持超级管理员、分公司管理员与普通用户多级鉴权与 JWT 保护。
- 📊 **现代化仪表盘**：Vue3 + Element Plus 构建的沉浸式后台，直观把控全网资产态势。
- 🔗 **生态与系统互联**：预留双域 AD 组织同步及用友 U8 采购/财务单号流转追踪。

---

## 📄 开源许可证

本项目采用 [GNU Affero General Public License v3.0 (AGPL-3.0)](LICENSE) 许可证开源。
