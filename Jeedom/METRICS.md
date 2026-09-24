# Метрики Ajax System для Jeedom

Цей документ описує телеметрію, яку містить пакет у каталозі `Jeedom`. Джерело — плагін Ajax System після коміту `af5a742`; зміни порівнюються з початковим станом `0ee0a70`.

Пакет містить **40 JSON-шаблонів пристроїв і 3 PHP-файли**: 30 нових шаблонів, 9 розширених і додатково `Transmitter.json`, у якому температура вже була додана до початкового коміту. PHP-файли: `core/class/ajaxSystem.class.php`, `core/php/jeeAjaxSystem.php` та `plugin_info/install.php`.

Це доповнення до наявного плагіна, а не повний перелік усіх пристроїв, які він підтримує. Нижче наведено всі інформаційні команди з 40 шаблонів пакета. Назви моделей відповідають точним іменам JSON-файлів та `deviceType` API.

## Як надходять дані

Під час звичайної синхронізації плагін отримує список обладнання та знімки стану пристроїв через хмару Jeedom. Подальші часткові оновлення надходять через наявний callback. Інформаційні команди зберігають відповідні поля цих повідомлень.

**Додавання метрик не створює окремого API-запиту для кожного показника, додаткових запитів синхронізації або нового періодичного опитування.** Сама ручна синхронізація, як і раніше, виконує свої звичайні запити виявлення та читання пристроїв.

Ключ `logicalId` — ідентифікатор команди Jeedom. Зазвичай він збігається з назвою поля API; `::` позначає вкладений шлях. Окремі старі ключі хмари, зокрема батарея та стан реле, нормалізуються у спільному обробнику.

## Спільні поля та позначення

У таблицях нижче використано такі набори; до рядка пристрою належать усі поля зазначеного набору разом із переліченими додатковими полями.

| Набір | Повний перелік `logicalId` | Значення |
| --- | --- | --- |
| **Події** | `sourceObjectName`, `event`, `eventCode` | Назва джерела, тип і код події, яку передала хмара |
| **Стан контролера** | `state` | Текстовий стан роботи пристрою; це не універсальний індикатор тривоги чи виходу реле |
| **Базове здоров’я** | `batteryChargeLevelPercentage`, `online`, `signalLevel`, `tampered`, `issuesCount` | Батарея (%), зв’язок, рівень сигналу, тампер, кількість несправностей |
| **Базові поля пожежних датчиків** | `state`, `sourceObjectName`, `event`, `eventCode`, `temperature`, `tampered`, `online`, `signalLevel` | Стан, події, температура (°C), тампер та зв’язок |

`online` і `tampered` — двійкові команди; `signalLevel` та `state` — текстові. Наявність поля в одному наборі не означає, що його має будь-який пристрій: кнопки, наприклад, не використовують набір «Базове здоров’я».

## Кнопки та брелок

| Моделі | Статус шаблону | Повний перелік інформаційних команд |
| --- | --- | --- |
| `Button`, `ButtonS` | Нові | **Події** + `batteryChargeLevelPercentage`, `issuesCount`, `firmwareVersion`, `buttonMode`, `batteryPingStatus`, `lastUpdateTimeSeconds` |
| `DoubleButton`, `SuperiorDoubleButtonG3` | Нові | **Події** + `batteryChargeLevelPercentage`, `issuesCount`, `firmwareVersion` |
| `SpaceControl` | Розширений | **Події** + `batteryChargeLevelPercentage`, `issuesCount`, `firmwareVersion` |

Батарея зберігається як число у відсотках з історією. `issuesCount` — числовий лічильник несправностей з історією; `firmwareVersion` — текстова версія прошивки. Нові шаблони кнопок і розширення SpaceControl не додають команди керування.

Додаткові поля Button/ButtonS:

| `logicalId` | Тип та зміст |
| --- | --- |
| `buttonMode` | Текстовий режим: `PANIC_BUTTON`, `SMART_BUTTON` або `INTERCONNECT_DELAY` |
| `batteryPingStatus` | Текстовий результат контролю батареї: `OK` або `NOT_OK` |
| `lastUpdateTimeSeconds` | Числова мітка останнього оновлення пристрою у секундах; команда прихована за замовчуванням |

**У цьому пакеті немає безперервного стану «кнопка натиснута», температури, рівня сигналу чи `online` для кнопок.** Короткі та довгі натискання не перетворюються на окремі команди автоматизації. `event`/`eventCode` показують лише події, які фактично переслала хмара.

JSON-шаблон не реєструє кнопку в Ajax і не змушує хмару повернути її у списку. Для появи обладнання звичайна синхронізація має отримати його ідентифікатор і придатну відповідь із назвою пристрою.

## Вода, температура та якість повітря

| Модель | Статус шаблону | Повний перелік інформаційних команд |
| --- | --- | --- |
| `WaterStop` | Розширений | **Події** + `valveState`, `batteryChargeLevelPercentage` |
| `Transmitter` | Додатково включений; температура була у початковому стані | **Події** + `online`, `signalLevel`, `temperature` |
| `LifeQualityLite` | Новий | `actualTemperature`, `actualHumidity`, `batteryChargeLevelPercentage`, `signalLevel`, `online`, `tampered`, `issuesCount` |

- **WaterStop:** `valveState` зберігає фактичне положення клапана як текст: `OPEN`, `CLOSED` або `INTERMEDIATE_STATE`. Це окреме показання, а не значення останньої команди відкриття/закриття. Батарея — %. Наявні дії `SWITCH_ON` і `SWITCH_OFF` залишаються у шаблоні.
- **Transmitter:** `temperature` — числова температура у °C з історією. Це температура самого модуля; вона не є виміром температури від довільного під’єднаного дротового датчика. Батарея, тампер та стан зовнішнього контакту окремими командами в цьому шаблоні не додані.
- **LifeQualityLite:** `actualTemperature` та `actualHumidity` використовують формулу `#value# / 10`, з одиницями °C та % відповідно. Масштаб температури підтверджено схемою API. **Масштаб вологості ще потрібно перевірити на реальному пристрої:** ділення на 10 перенесено з наявного шаблону LifeQuality, але живі дані LifeQualityLite не підтверджені. CO₂ та команда `actualCO2` відсутні.

## ReX: повторювачі сигналу

| Модель | Статус шаблону | Повний перелік інформаційних команд |
| --- | --- | --- |
| `RangeExtender` (ReX) | Розширений: раніше шаблон не мав команд | **Події** + **Стан контролера** + **Базове здоров’я** + `externallyPowered`, `temperature` |
| `RangeExtender2` (ReX 2) | Новий | Усі поля `RangeExtender` + `radioConnectionOk`, `dataChannelOk`, `dataChannelSignalQuality`, `networkDetails::ethernet::enabled`, `networkDetails::ethernet::connectionOk` |
| `SuperiorRexG3` | Новий | Усі поля `RangeExtender2` + `jewellerAntennaStatus`, `wingsAntennaStatus` |

`externallyPowered` показує зовнішнє живлення; `temperature` — °C. `radioConnectionOk` і `dataChannelOk` — двійкові статуси радіозв’язку та каналу даних; `dataChannelSignalQuality` — текстова якість каналу. Вкладені Ethernet-команди показують, чи ввімкнений інтерфейс і чи встановлений зв’язок. Статуси антен Superior ReX G3 зберігаються як текст.

Цей пакет не додає команд рівня радіошуму, IP-адреси або керування мережею.

## MultiTransmitter: здоров’я контролера

Усі три шаблони нові. Вони описують **сам контролер MultiTransmitter**, а не кожен під’єднаний дротовий вхід.

| Модель | Повний перелік інформаційних команд |
| --- | --- |
| `MultiTransmitter` | **Події** + **Стан контролера** + **Базове здоров’я** + `externallyPowered`, `charging`, `batteryMalfunction`, `externalDevicePowerFailure`, `externalFireAlarmPowerFailure` |
| `MultiTransmitterFibra` | Усі поля `MultiTransmitter` + `sensorsUnderVoltage`, `fireNotifiersUnderVoltage`, `maxPowerTestResult`, `malfunctionStates` |
| `SuperiorMultiTransmitterG3` | Усі поля `MultiTransmitter` + `dataChannelOk`, `dataChannelSignalQuality`, `sensorsState`, `fireNotifiersState`, `malfunctionStates` |

| `logicalId` | Значення |
| --- | --- |
| `externallyPowered` | Наявність зовнішнього живлення |
| `charging` | Заряджання батареї |
| `batteryMalfunction` | Несправність батареї |
| `externalDevicePowerFailure` | Проблема живлення під’єднаних пристроїв |
| `externalFireAlarmPowerFailure` | Проблема живлення пожежних пристроїв |
| `sensorsUnderVoltage`, `fireNotifiersUnderVoltage` | Текстові статуси живлення відповідних ліній Fibra |
| `maxPowerTestResult` | Текстовий результат тесту максимального навантаження |
| `dataChannelOk`, `dataChannelSignalQuality` | Текстові статуси каналу даних Superior MultiTransmitter G3 |
| `sensorsState`, `fireNotifiersState` | Текстові стани під’єднаних груп пристроїв |
| `malfunctionStates` | Список несправностей, записаний як JSON-текст |

Тип `dataChannelOk` тут навмисно текстовий, відповідно до шаблону цієї моделі; у шаблонах ReX 2 він двійковий. Не слід однаково інтерпретувати всі однойменні поля різних сімейств.

## FireProtect першого покоління

Обидва шаблони розширені. Кожен містить **Базові поля пожежних датчиків** та такі додаткові команди:

| Модель | Повний перелік додаткових тривог |
| --- | --- |
| `FireProtect` | `smokeAlarmDetected`, `temperatureAlarmDetected`, `highTemperatureDiffDetected` |
| `FireProtectPlus` | `smokeAlarmDetected`, `temperatureAlarmDetected`, `highTemperatureDiffDetected`, `coAlarmDetected` |

Ці команди двійкові: дим, висока температура, швидке зростання температури та CO відповідно. Вони не є числовими вимірами концентрації диму або CO.

## FireProtect 2: точна відповідність датчикам моделі

Кожна модель нижче містить **Базові поля пожежних датчиків**. Таблиця задає повний перелік її додаткових тривог; інші тривоги у шаблон не додаються. Усі ці додаткові команди **текстові**, а не двійкові.

Шаблони `FireProtect2`, `FireProtect2Plus` і `FireProtect2PlusSb` розширені. Решта **20** шаблонів FireProtect 2 у цій таблиці — нові.

| Моделі, точний `deviceType` | Група датчиків | Додаткові `logicalId` |
| --- | --- | --- |
| `FireProtect2`, `FireProtect2Sb`, `FireProtect2HsAc` | Дим, нагрівання, швидке зростання температури | `smokeAlarm`, `tempAlarm`, `tempHighDiffAlarm` |
| `FireProtect2Plus`, `FireProtect2PlusSb`, `FireProtect2HscAc` | Дим, нагрівання, швидке зростання температури, CO | `smokeAlarm`, `tempAlarm`, `tempHighDiffAlarm`, `coAlarm`, `criticalCoAlarm` |
| `FireProtect2Hrb`, `FireProtect2Hsb`, `FireProtect2HAc` | Нагрівання, швидке зростання температури | `tempAlarm`, `tempHighDiffAlarm` |
| `FireProtect2Crb`, `FireProtect2Csb`, `FireProtect2CAc`, `FireProtect2CRbUl` | Лише CO | `coAlarm`, `criticalCoAlarm` |
| `FireProtect2Hcrb`, `FireProtect2Hcsb`, `FireProtect2HcAc` | Нагрівання, швидке зростання температури, CO | `tempAlarm`, `tempHighDiffAlarm`, `coAlarm`, `criticalCoAlarm` |
| `FireProtect2HRbUl` | Нагрівання | `tempAlarm` |
| `FireProtect2HsRbUl`, `FireProtect2HsSbUl`, `FireProtect2HsAcUl` | Дим, критична тривога диму, нагрівання | `smokeAlarm`, `criticalSmokeAlarm`, `tempAlarm` |
| `FireProtect2HscRbUl`, `FireProtect2HscSbUl`, `FireProtect2HscAcUl` | Дим, критична тривога диму, нагрівання, CO | `smokeAlarm`, `criticalSmokeAlarm`, `tempAlarm`, `coAlarm`, `criticalCoAlarm` |

Приклад значень `smokeAlarm`: `SMOKE_ALARM_DETECTED` та `SMOKE_ALARM_NOT_DETECTED`. Сценарії мають порівнювати явні текстові значення, а не трактувати будь-який непорожній рядок як тривогу.

Моделі лише з CO не отримують `smokeAlarm` чи `tempAlarm`. UL-варіанти у наведених шаблонах не мають `tempHighDiffAlarm`; для відповідних димових UL-моделей є `criticalSmokeAlarm`. Базова `temperature` не означає, що модель має окремий канал тривоги перегріву.

## WallSwitch та наявний Relay

У розширеному шаблоні `WallSwitch` (`WallSwitch.json`) повний перелік інформаційних команд: **Події** + `realState`, `voltage`, `powerWtH`, `currentMA`. Дії `SWITCH_ON` і `SWITCH_OFF` збережені. Новою є видима команда «Etat» / `realState`, тип `binary`, з історією: **1 — увімкнено, 0 — вимкнено**.

`voltage`, `powerWtH` і `currentMA` — наявні ключі старого шаблону/проксі. Пакет їх зберігає; він не додає перерахунок одиниць або відповідність усіх нових API-полів електричних вимірів. Особливо не слід трактувати назву `powerWtH` як підтвердження одиниці миттєвої потужності.

Спільний PHP-обробник тепер перетворює масив API `switchState` у `realState` для **WallSwitch і Relay**:

| Прапор API | Значення `realState` |
| --- | --- |
| `SWITCHED_ON` | 1 |
| `SWITCHED_OFF` | 0 |
| `OFF_TOO_LOW_VOLTAGE`, `OFF_HIGH_VOLTAGE`, `OFF_HIGH_TEMPERATURE` | 0 |
| `OFF_HIGH_CURRENT`, `OFF_SHORT_CIRCUIT` | 0 лише для WallSwitch |

Відсутній, порожній, невідомий або суперечливий набір прапорів, а також `CONTACT_HANG` без явного стану, не породжують нове значення. Окремо передане старе `realState` callback обробляється як раніше, з інверсією. Якщо повідомлення містить обидва формати й `switchState` однозначний, він має пріоритет і повторно не інвертується.

Це показання пристрою, а не результат припущення після натискання On/Off у Jeedom.

**`Relay.json` не входить до пакета:** наявна команда `realState` отримує нову поведінку через оновлений PHP. **`Socket.json` і `SOCKET.json` також не входять до пакета:** розширення цього перетворення на розетки у версію `af5a742` не включене.

## Батарея, вкладені поля та списки

Обробник узгоджує ключі батареї `batteryCharge`, `batteryChargeLevelPercentage` і `battery::chargeLevelPercentage`, включно з вкладеним об’єктом `battery.chargeLevelPercentage`. Значення від 0 до 100 оновлюють наявні відповідні команди та батарейний статус Jeedom. Це не створює команд батареї у шаблонах, де вони не визначені.

Для вкладених полів, наприклад `networkDetails::ethernet::connectionOk`, читається відповідний шлях у даних. Масиви зберігаються як JSON-текст лише у текстових командах; їх не перетворюють на довільне двійкове значення.

## Оновлення існуючого обладнання та межі перевірки

Оновлення плагіна або звичайна синхронізація додають відсутні інформаційні команди за `logicalId`. Наявні ідентифікатори команд, назви, історія, порядок і налаштування зберігаються. Через це нові значення видимості чи історизації з шаблону не перевизначають налаштування вже наявної команди.

Після ручного копіювання файлів потрібна звичайна синхронізація, щоб створити відсутні команди та заповнити доступні значення знімка. Команда може залишитися без значення, якщо хмара не передає потрібне поле.

- Поля залежать від моделі, прошивки та того, що повертає проксі Jeedom. Наявність поля в офіційній схемі не гарантує його фактичної доставки цій інсталяції.
- Часткове оновлення без певного поля зберігає попереднє значення. Незмінне показання не доводить, що було отримано новий вимір.
- Пакет не додає окремого контролю віку кожної метрики, відновлення пропущених подій або гарантованої частоти оновлення.
- Кнопки не отримують окремої автоматизації натискань; MultiTransmitter не отримує нових каналів дротових входів; LifeQualityLite не отримує CO₂.
- Відповідності перевірено за [офіційною схемою Ajax API v1.152.0](https://api.ajax.systems/api/swagger/history/1.152.0/swagger.yaml) та JSON-шаблонами плагіна. Повноту живої доставки всіх полів і масштаб вологості LifeQualityLite потрібно перевірити на обладнанні.
