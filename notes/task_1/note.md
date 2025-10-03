# Обновление архитектуры системы "Кинобездна"

- **Выделение сервиса "movies"**: Это отдельный микросервис в рамках bounded context "Content Catalog", отвечающий только за метаданные фильмов (жанры, актёры, оценки). Он интегрируется с остальными сервисами, но фокус на нём как на первом шаге миграции.
- **Strangler Fig паттерн**: Прокси-сервис (API Gateway) "душит" монолит, постепенно перенаправляя трафик на новые сервисы. Старый монолит остаётся активным, но трафик на него уменьшается.
- **Прокси-сервис (API Gateway)**: Расширенная роль — не только маршрутизация и адаптация под устройства, но и проксирование для Strangler Fig. Он проверяет аутентификацию и перенаправляет запросы (синхронно через REST, асинхронно через события).
- **Другие домены**: Пока остаются в монолите, но API Gateway готов для их постепенного выделения (например, следующий шаг — Billing & Payments). Интеграции с "movies" через события (Message Broker).
- **Миграция и мониторинг**: Добавлен этап мониторинга (ELK) для отслеживания трафика, ошибок и производительности во время переключения. Рекомендация: Начать с read-only операций на "movies", затем write.

## Обновлённая диаграмма контейнеров в нотации C4
Диаграмма отражает интеграцию "movies" сервиса и роль API Gateway как прокси для Strangler Fig.

```plantuml
@startuml
!include https://raw.githubusercontent.com/plantuml-stdlib/C4-PlantUML/master/C4_Container.puml

Person(user, "Пользователь", "Использует мобильные устройства, ноутбуки, смарт ТВ")

System_Boundary(kino_bezdna, "Кинобездна") {
    Container(api_gateway, "API Gateway (Прокси)", "Nginx/Kong", "Единая точка вызова: маршрутизация, аутентификация, адаптация под устройства, Strangler Fig прокси (переключение трафика)")
    
    Container(monolith, "Legacy Monolith", "Go + PostgreSQL", "Старый монолит: содержит все домены, включая исходные метаданные фильмов (для Strangler Fig)")
    
    Container(movies_service, "Movies Service", "Go, REST API", "Новый сервис метаданных фильмов (bounded context: Content Catalog - movies)")
    ContainerDb(movies_db, "Movies Database", "PostgreSQL", "Метаданные фильмов (жанры, актёры, оценки)")
    
    Container(iam_service, "IAM Service", "Go, REST API", "Управление пользователями, аутентификация (bounded context: IAM)")
    ContainerDb(iam_db, "IAM Database", "PostgreSQL", "Данные пользователей")
    
    Container(profile_service, "User Profile Service", "Go, REST API", "Управление профилями, избранным (bounded context: User Profile)")
    ContainerDb(profile_db, "Profile Database", "PostgreSQL", "Профили пользователей")
    
    Container(billing_service, "Billing & Payments Service", "Go, REST API", "Управление подписками, платежами, скидками (bounded context: Billing & Payments)")
    ContainerDb(billing_db, "Billing Database", "PostgreSQL", "Подписки и платежи")
    
    Container(rec_service, "Recommendation Engine", "External Service", "Генерация рекомендаций на основе оценок (bounded context: Recommendation)")
    ContainerDb(rec_db, "Recommendation Database", "External DB", "Данные для рекомендаций")
    
    Container(message_broker, "Message Broker", "RabbitMQ", "Асинхронная коммуникация (события)")
    Container(cache, "Cache", "Redis", "Кэширование данных")
    Container(monitoring, "Monitoring", "ELK Stack", "Логи и метрики (отслеживание трафика в Strangler Fig)")
}

Rel(user, api_gateway, "Использует", "HTTP/REST")

Rel(api_gateway, monolith, "Перенаправляет трафик", "HTTP (Strangler Fig - legacy path)")
Rel(api_gateway, movies_service, "Перенаправляет трафик", "HTTP (Strangler Fig - new path)")

Rel(api_gateway, iam_service, "Маршрутизация (будущая миграция)", "HTTP")
Rel(api_gateway, profile_service, "Маршрутизация (будущая миграция)", "HTTP")
Rel(api_gateway, billing_service, "Маршрутизация (будущая миграция)", "HTTP")
Rel(api_gateway, rec_service, "Маршрутизация рекомендаций", "HTTP")

Rel(api_gateway, cache, "Кэширование", "Redis")

Rel(monolith, movies_db, "Старый доступ (для синхронизации)", "SQL (временно)")

Rel(movies_service, movies_db, "Чтение/запись", "SQL")
Rel(iam_service, iam_db, "Чтение/запись", "SQL")
Rel(profile_service, profile_db, "Чтение/запись", "SQL")
Rel(billing_service, billing_db, "Чтение/запись", "SQL")
Rel(rec_service, rec_db, "Чтение/запись", "SQL")

Rel(movies_service, message_broker, "Публикация событий (оценки)", "AMQP")
Rel(billing_service, message_broker, "Публикация событий (платежи)", "AMQP")
Rel(message_broker, rec_service, "Потребление событий для обновления модели", "AMQP")

Rel(monitoring, api_gateway, "Мониторинг трафика", "Logs/Metrics")
Rel(monitoring, monolith, "Мониторинг legacy", "Logs/Metrics")
Rel(monitoring, movies_service, "Мониторинг нового сервиса", "Logs/Metrics")
Rel(monitoring, iam_service, "Мониторинг", "Logs/Metrics")
Rel(monitoring, profile_service, "Мониторинг", "Logs/Metrics")
Rel(monitoring, billing_service, "Мониторинг", "Logs/Metrics")
Rel(monitoring, rec_service, "Мониторинг", "Logs/Metrics")

@enduml
```

## Пояснения к обновлениям
- **Strangler Fig реализация**: API Gateway действует как прокси, перенаправляя трафик. Например, для запросов на метаданные фильмов: 70% трафика идёт на "movies_service", 30% — на монолит. Это минимизирует риски.
- **Интеграция "movies"**: Новый сервис синхронизирует данные с монолитом (временно, через ETL или события) для консистентности. Асинхронные события от "movies" (например, новая оценка) идут в Recommendation Engine.
- **Другие сервисы**: Пока в монолите, но API Gateway готов для их выделения.
- **Преимущества**: Бесшовный переход, возможность тестирования, откат при ошибках. Мониторинг ELK отслеживает метрики трафика.
- **Следующие шаги**: После "movies" применить то же к другим доменам. Если нужны детали по логике или код-примеры, дайте знать!